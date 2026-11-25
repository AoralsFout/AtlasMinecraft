package world

import (
	"context"
	"maps"
	"math"
	"sort"
	"time"
)

// ChunkPosition is a horizontal position in chunk coordinates.
type ChunkPosition struct{ X, Z int32 }

type SimulationPlayer struct {
	X, Z      float64
	Spectator bool
}

// Ticket levels propagate in squares (Chebyshev distance). The outer two
// rings supply the neighbors required for block and entity ticking.
type simulationTickets struct {
	spawn    spawnChunkTicket
	centers  map[ChunkPosition]struct{}
	distance int32
	chunks   map[chunkKey]simulationChunkTicket
}
type simulationChunkTicket struct {
	distance int32 // generation priority: nearest source
	level    int32 // strongest ticket: 31 entity, 32 block, 33 loaded
}

const worldChunkLimit = 30_000_000 / 16

func (t *simulationTickets) addCenter(center ChunkPosition, entityRadius int32) {
	// Border chunks still need their two outer rings for readiness.
	radius := entityRadius + 2
	for x := max(center.X-radius, -worldChunkLimit-2); x <= min(center.X+radius, worldChunkLimit+1); x++ {
		for z := max(center.Z-radius, -worldChunkLimit-2); z <= min(center.Z+radius, worldChunkLimit+1); z++ {
			key := chunkKey{x, z}
			d := max(x-center.X, center.X-x, z-center.Z, center.Z-z)
			ticket := simulationChunkTicket{distance: d, level: 31 + d - entityRadius}
			if old, ok := t.chunks[key]; ok {
				ticket.distance = min(old.distance, ticket.distance)
				ticket.level = min(old.level, ticket.level)
			}
			t.chunks[key] = ticket
		}
	}
}

// Published maps are immutable. The caller holds blockUpdateMu.
func (w *World) rebuildSimulationTicketsLocked() {
	if tickets := w.simulation.Load(); tickets != nil {
		w.publishSimulationTickets(tickets.centers, tickets.distance)
	} else {
		w.publishSimulationTickets(nil, 0)
	}
}

func (w *World) publishSimulationTickets(centers map[ChunkPosition]struct{}, distance int32) *simulationTickets {
	spawn := w.spawnTicketLocked()
	if len(centers) == 0 && len(w.forced.positions) == 0 && len(w.portalTicks) == 0 && spawn.radius == 0 {
		w.simulation.Store(nil)
		return nil
	}
	tickets := &simulationTickets{spawn: spawn, centers: centers, distance: distance, chunks: make(map[chunkKey]simulationChunkTicket)}
	if spawn.radius > 0 {
		tickets.addCenter(spawn.center, spawn.radius-1)
	}
	for center := range centers {
		tickets.addCenter(center, distance)
	}
	for center := range w.forced.positions {
		tickets.addCenter(center, 0)
	}
	for center := range w.portalTicks {
		tickets.addCenter(center, 1) // PORTAL at level 30
	}
	w.simulation.Store(tickets)
	select {
	case w.simulationWake <- struct{}{}:
	default:
	}
	return tickets
}

// TickPlayers updates this dimension's player tickets and executes one game
// tick. Changes to the ticket set wake asynchronous terrain preparation.
func (w *World) TickPlayers(players []SimulationPlayer, distance int32) error {
	return w.tickPlayers(players, distance, false)
}

// TickServerPlayers advances the clock for the Overworld and runs each
// derived dimension's queues at the same age. The server ticks Overworld first.
func (w *World) TickServerPlayers(players []SimulationPlayer, distance int32) error {
	return w.tickPlayers(players, distance, true)
}

func (w *World) tickPlayers(players []SimulationPlayer, distance int32, serverTick bool) error {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	w.simulationPlayers = append(w.simulationPlayers[:0], players...)
	tickets := w.simulation.Load()
	entityPhase := true
	if len(players) == 0 && len(w.forced.positions) == 0 {
		entityPhase = w.emptyTicks < 300
		if entityPhase {
			w.emptyTicks++
		}
		if w.spawnRadius == 0 && len(w.portalTicks) == 0 {
			w.simulation.Store(nil)
			return w.tickLocked(TickChunks{}, true, serverTick)
		}
	} else {
		w.emptyTicks = 0
	}
	centers := make(map[ChunkPosition]struct{}, len(players))
	for _, p := range players {
		centers[ChunkPosition{int32(math.Floor(p.X / 16)), int32(math.Floor(p.Z / 16))}] = struct{}{}
	}
	if tickets == nil || tickets.distance != distance || !maps.Equal(tickets.centers, centers) {
		tickets = w.publishSimulationTickets(centers, distance)
	}
	chunks := TickChunks{
		Blocks: func(x, z int32) bool {
			d, ok := tickets.chunks[chunkKey{x, z}]
			return ok && d.level <= 32
		},
	}
	if len(players) > 0 {
		chunks.Random = w.randomTickEligibleLocked
	}
	if entityPhase {
		chunks.Entities = w.entityTickEligible
	}
	return w.tickLocked(chunks, true, serverTick)
}

func (w *World) entityTickEligible(x, z int32) bool {
	tickets := w.simulation.Load()
	if tickets == nil {
		return false
	}
	d, ok := tickets.chunks[chunkKey{x, z}]
	return ok && d.level <= 31 && w.scheduledTicks.neighborhoodLoaded(x, z, 2)
}

// RandomTickEligible reports the 1.21.1 chunk gate under the latest player
// tickets. The horizontal player radius is strict and independent of height.
func (w *World) RandomTickEligible(x, z int32) bool {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	return w.randomTickEligibleLocked(x, z)
}

func (w *World) randomTickEligibleLocked(x, z int32) bool {
	if !w.entityTickEligible(x, z) {
		return false
	}
	for _, player := range w.simulationPlayers {
		dx, dz := float64(x)*16+8-player.X, float64(z)*16+8-player.Z
		if !player.Spectator && dx*dx+dz*dz < 128*128 {
			return true
		}
	}
	return false
}

func (w *World) simulationDistance(key chunkKey) (int32, bool) {
	if tickets := w.simulation.Load(); tickets != nil {
		ticket, ok := tickets.chunks[key]
		return ticket.distance, ok
	}
	return 0, false
}

type simulationChunkLoads struct{ chunks []chunkKey }

// The clock publishes immutable tickets. This worker posts one batch per
// region, without waiting for terrain. Replay batches periodically so a
// transient load failure can recover while a player stays in the same chunk.
func (w *World) requestSimulationChunks() {
	defer w.wg.Done()
	retry := time.NewTicker(5 * time.Second)
	defer retry.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-w.simulationWake:
		case <-retry.C:
		}
		tickets := w.simulation.Load()
		if tickets == nil {
			continue
		}
		byRegion := make(map[chunkKey][]chunkKey)
		var regions []chunkKey
		for key := range tickets.chunks {
			region := chunkKey{key.x >> RegionChunkBits, key.z >> RegionChunkBits}
			if _, ok := byRegion[region]; !ok {
				regions = append(regions, region)
			}
			byRegion[region] = append(byRegion[region], key)
		}
		sort.Slice(regions, func(i, j int) bool {
			a, b := regions[i], regions[j]
			return a.x < b.x || a.x == b.x && a.z < b.z
		})
		for _, region := range regions {
			if w.simulation.Load() != tickets {
				break
			}
			keys := byRegion[region]
			sort.Slice(keys, func(i, j int) bool {
				a, b := keys[i], keys[j]
				da, db := tickets.chunks[a].distance, tickets.chunks[b].distance
				if da != db {
					return da < db
				}
				return a.x < b.x || a.x == b.x && a.z < b.z
			})
			if err := w.post(context.Background(), region.x<<RegionChunkBits, region.z<<RegionChunkBits, simulationChunkLoads{keys}); err != nil {
				return
			}
		}
	}
}
