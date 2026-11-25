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

// Player tickets in 1.21.1 cover a square (Chebyshev distance). The block
// ring and the loaded-only ring supply the 3x3 and 5x5 ticking neighborhoods.
type simulationTickets struct {
	centers  map[ChunkPosition]struct{}
	distance int32
	chunks   map[chunkKey]int32 // minimum distance to a player, across all tickets
}

func newSimulationTickets(centers map[ChunkPosition]struct{}, distance int32) *simulationTickets {
	tickets := &simulationTickets{centers: maps.Clone(centers), distance: distance, chunks: make(map[chunkKey]int32)}
	const worldChunkLimit = 30_000_000 / 16
	radius := distance + 2
	for center := range centers {
		for x := max(center.X-radius, -worldChunkLimit); x <= min(center.X+radius, worldChunkLimit-1); x++ {
			for z := max(center.Z-radius, -worldChunkLimit); z <= min(center.Z+radius, worldChunkLimit-1); z++ {
				key := chunkKey{x, z}
				d := max(x-center.X, center.X-x, z-center.Z, center.Z-z)
				if previous, ok := tickets.chunks[key]; !ok || d < previous {
					tickets.chunks[key] = d
				}
			}
		}
	}
	return tickets
}

// TickPlayers updates this dimension's player tickets and executes one game
// tick. Changes to the ticket set wake asynchronous terrain preparation.
func (w *World) TickPlayers(players []SimulationPlayer, distance int32) error {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	w.simulationPlayers = append(w.simulationPlayers[:0], players...)
	tickets := w.simulation.Load()
	if len(players) == 0 {
		w.simulation.Store(nil)
		return w.tickLocked(TickChunks{}, true)
	}
	centers := make(map[ChunkPosition]struct{}, len(players))
	for _, p := range players {
		centers[ChunkPosition{int32(math.Floor(p.X / 16)), int32(math.Floor(p.Z / 16))}] = struct{}{}
	}
	if tickets == nil || tickets.distance != distance || !maps.Equal(tickets.centers, centers) {
		tickets = newSimulationTickets(centers, distance)
		w.simulation.Store(tickets)
		select {
		case w.simulationWake <- struct{}{}:
		default:
		}
	}
	return w.tickLocked(TickChunks{
		Blocks: func(x, z int32) bool {
			d, ok := tickets.chunks[chunkKey{x, z}]
			return ok && d <= distance+1
		},
		Entities: w.entityTickEligible,
	}, true)
}

func (w *World) entityTickEligible(x, z int32) bool {
	tickets := w.simulation.Load()
	if tickets == nil {
		return false
	}
	d, ok := tickets.chunks[chunkKey{x, z}]
	return ok && d <= tickets.distance && w.scheduledTicks.neighborhoodLoaded(x, z, 2)
}

// RandomTickEligible reports the 1.21.1 chunk gate under the latest player
// tickets. The horizontal player radius is strict and independent of height.
func (w *World) RandomTickEligible(x, z int32) bool {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
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
		distance, ok := tickets.chunks[key]
		return distance, ok
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
				da, db := tickets.chunks[a], tickets.chunks[b]
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
