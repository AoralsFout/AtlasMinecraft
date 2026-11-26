package world

import (
	"atlas/internal/tickqueue"
	"atlas/internal/worldgen"
	"crypto/rand"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
)

//go:embed entity_sizes_1.21.1.json
var entitySizesJSON []byte

var entitySizes = func() map[string][2]float32 {
	var sizes map[string][2]float32
	if err := json.Unmarshal(entitySizesJSON, &sizes); err != nil {
		panic(err)
	}
	return sizes
}()

const LightningEntity = "minecraft:lightning_bolt"

type lightningBolt struct {
	entity        WorldEntity
	visualOnly    bool
	life, flashes int32
	random        *worldgen.LegacyRandom
}

// Only the topmost non-air block in a column can be a lightning rod POI.
// Both WORLD_SURFACE and the block state come from the same loaded chunk.
func (w *World) closestLightningRod(p worldgen.FeaturePosition) (worldgen.FeaturePosition, bool, error) {
	const radius = int32(128)
	best := radius*radius + 1
	var rod worldgen.FeaturePosition
	owner := w.scheduledTicks
	owner.mu.Lock()
	var chunks []ChunkPosition
	for z := (p.Z - radius) >> 4; z <= (p.Z+radius)>>4; z++ {
		for x := (p.X - radius) >> 4; x <= (p.X+radius)>>4; x++ {
			if owner.chunks[tickqueue.ChunkKey(x, z)] != nil {
				chunks = append(chunks, ChunkPosition{x, z})
			}
		}
	}
	owner.mu.Unlock()
	for _, c := range chunks {
		err := w.fallingEntityOperation(c.X, c.Z, func(_ *Region, ch *Chunk) error {
			for z := 0; z < 16; z++ {
				for x := 0; x < 16; x++ {
					y := ch.Height(x, z) - 1
					if y < ch.minY || !lightningRodStates[ch.GetBlock(x, z, y)] {
						continue
					}
					bx, bz := c.X*16+int32(x), c.Z*16+int32(z)
					dx, dy, dz := p.X-bx, p.Y-y, p.Z-bz
					distance := dx*dx + dy*dy + dz*dz
					if distance < best {
						best = distance
						rod = worldgen.FeaturePosition{X: bx, Y: y + 1, Z: bz}
					}
				}
			}
			return nil
		})
		if err != nil {
			return rod, false, err
		}
	}
	return rod, best <= radius*radius, nil
}

var lightningRodStates = func() [MaxBlockState + 1]bool {
	var flags [MaxBlockState + 1]bool
	for _, facing := range []string{"up", "down", "east", "west", "north", "south"} {
		for _, powered := range []string{"false", "true"} {
			for _, water := range []string{"false", "true"} {
				id, err := BlockStateID("minecraft:lightning_rod", map[string]string{"facing": facing, "powered": powered, "waterlogged": water})
				if err != nil {
					panic(err)
				}
				flags[id] = true
			}
		}
	}
	return flags
}()

type strikeCandidate struct {
	pos           [3]float64
	width, height float64
}

func (r *fluidRuntime) lightningTarget(p worldgen.FeaturePosition) (worldgen.FeaturePosition, error) {
	w := r.world
	if err := w.fallingEntityOperation(p.X>>4, p.Z>>4, func(_ *Region, c *Chunk) error {
		p.Y = c.motionBlockingHeight(int(p.X&15), int(p.Z&15))
		return nil
	}); err != nil {
		return p, err
	}
	if rod, found, err := w.closestLightningRod(p); found || err != nil {
		return rod, err
	}
	var candidates []strikeCandidate
	// Include nearby loaded living entities whose boxes can intersect the
	// inflated strike column. Nonliving entities have no saved Health tag.
	for cz := (p.Z - 12) >> 4; cz <= (p.Z+12)>>4; cz++ {
		for cx := (p.X - 12) >> 4; cx <= (p.X+12)>>4; cx++ {
			err := w.fallingEntityOperation(cx, cz, func(_ *Region, c *Chunk) error {
				for _, e := range c.entities {
					health, alive := e.Data["Health"].Value.(float32)
					if !alive || health <= 0 {
						continue
					}
					size := entitySizes[e.Type]
					if age, ok := e.Data.Number("Age"); ok && age < 0 {
						size[0] *= 0.5
						size[1] *= 0.5
					}
					candidates = append(candidates, strikeCandidate{e.Position, float64(size[0]), float64(size[1])})
				}
				return nil
			})
			if err != nil && !errors.Is(err, ErrChunkNotLoaded) {
				return p, err
			}
		}
	}
	for _, player := range w.simulationPlayers {
		candidates = append(candidates, strikeCandidate{[3]float64{player.X, player.Y, player.Z}, float64(float32(0.6)), float64(float32(1.8))})
	}
	var exposed []worldgen.FeaturePosition
	for _, entity := range candidates {
		x, y, z := entity.pos[0], entity.pos[1], entity.pos[2]
		half := entity.width / 2
		if x+half <= float64(p.X-3) || x-half >= float64(p.X+4) || z+half <= float64(p.Z-3) || z-half >= float64(p.Z+4) || y+entity.height <= float64(p.Y-3) || y >= float64(w.MaxBuildHeight()+4) {
			continue
		}
		at := worldgen.FeaturePosition{X: int32(math.Floor(x)), Y: int32(math.Floor(y)), Z: int32(math.Floor(z))}
		sky, _, err := r.rawLightAt(at)
		if err != nil {
			return p, err
		}
		if sky == 15 {
			exposed = append(exposed, at)
		}
	}
	if len(exposed) > 0 {
		return exposed[w.tickRandom.NextIntBound(int32(len(exposed)))], nil
	}
	if p.Y == w.MinBuildHeight()-1 {
		p.Y += 2
	}
	return p, nil
}

func (r *fluidRuntime) naturalLightning(column worldgen.FeaturePosition) error {
	p, err := r.lightningTarget(column)
	if err != nil {
		return err
	}
	raining, err := r.rainingAt(p)
	if err != nil || !raining {
		return err
	}
	id, seed, err := newLightningUUID()
	if err != nil {
		return err
	}
	var horse *WorldEntity
	if r.world.MobSpawning() && r.world.tickRandom.NextDouble() < float64(r.world.localDifficulty(p))*0.01 {
		below := p
		below.Y--
		state, err := r.BlockAt(below)
		if err != nil {
			return err
		}
		if state.Name != "minecraft:lightning_rod" {
			horse, err = r.spawnSkeletonTrap(p)
			if err != nil {
				return err
			}
		}
	}
	entity := WorldEntity{Type: LightningEntity, UUID: id, RuntimeID: NextRuntimeEntityID(), Position: [3]float64{float64(p.X) + 0.5, float64(p.Y), float64(p.Z) + 0.5}}
	r.world.addLightning(entity, horse != nil, seed)
	if r.lightningAdded != nil {
		r.lightningAdded(entity, horse)
	}
	return nil
}

func (w *World) addLightning(entity WorldEntity, visualOnly bool, seed int64) {
	rng := worldgen.NewLegacyRandom(seed)
	rng.NextLong()
	w.bolts = append(w.bolts, lightningBolt{entity: entity, visualOnly: visualOnly, life: 2, flashes: 1 + rng.NextIntBound(3), random: rng})
}

func newLightningUUID() (id [16]byte, seed int64, err error) {
	_, err = rand.Read(id[:])
	seed = int64(binary.LittleEndian.Uint64(id[:8]))
	id[6] = id[6]&15 | 64
	id[8] = id[8]&63 | 128
	return
}

func (w *World) LightningEntities() []WorldEntity {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	var snapshots []WorldEntity
	for _, bolt := range w.bolts {
		snapshots = append(snapshots, bolt.entity)
	}
	return snapshots
}

func (w *World) tickLightning(eligible func(int32, int32) bool) error {
	if len(w.bolts) == 0 {
		return nil
	}
	r := fluidRuntime{world: w, settings: w.fluidSettings()}
	live := w.bolts[:0]
	var failures []error
	for _, bolt := range w.bolts {
		xyz := bolt.entity.Position
		p := worldgen.FeaturePosition{X: int32(math.Floor(xyz[0])), Y: int32(math.Floor(xyz[1]-1e-6)), Z: int32(math.Floor(xyz[2]))}
		if !eligible(p.X>>4, p.Z>>4) {
			if w.scheduledTicks.neighborhoodLoaded(p.X>>4, p.Z>>4, 0) {
				live = append(live, bolt)
			}
			continue
		}
		if bolt.life == 2 {
			if err := r.powerLightningRod(p); err != nil {
				failures = append(failures, err)
				live = append(live, bolt)
				continue
			}
		}
		bolt.life--
		if bolt.life < 0 {
			if bolt.flashes == 0 {
				continue
			}
			if bolt.life < -bolt.random.NextIntBound(10) {
				bolt.flashes--
				bolt.life = 1
				bolt.random.NextLong()
			}
		}
		live = append(live, bolt)
	}
	clear(w.bolts[len(live):])
	w.bolts = live
	return errors.Join(failures...)
}

func (r *fluidRuntime) powerLightningRod(p worldgen.FeaturePosition) error {
	state, err := r.BlockAt(p)
	if err != nil || state.Name != "minecraft:lightning_rod" {
		return err
	}
	if state.Properties["powered"] == "true" {
		if err := r.ScheduleBlockTick(p, state.Name, 8, 0); err != nil {
			return err
		}
	} else {
		state.Properties["powered"] = "true"
		id, err := BlockStateID(state.Name, state.Properties)
		if err != nil {
			return err
		}
		changed, err := r.world.editBlock(setBlockMsg{x: p.X, y: p.Y, z: p.Z, state: id, releaseBlock: state.Name, releaseDelay: 8, loadedOnly: true})
		if err != nil {
			return err
		}
		if err := r.world.updateBlockNeighbors(changed); err != nil {
			return err
		}
	}
	if err := r.NotifyRodSupport(p, state); err != nil {
		return err
	}
	axis := int32(1)
	switch state.Properties["facing"] {
	case "east", "west":
		axis = 0
	case "north", "south":
		axis = 2
	}
	return r.FluidLevelEvent(p, 3002, axis)
}

func (r *fluidRuntime) NotifyRodSupport(p worldgen.FeaturePosition, state worldgen.SurfaceBlockState) error {
	support := fluidAdjacent(p, oppositeFace(FacingFace(state.Properties["facing"])))
	return r.enqueue(&fluidNeighborUpdate{position: support, multi: true, loadedOnly: true})
}
