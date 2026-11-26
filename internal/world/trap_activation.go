package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	_ "embed"
	"encoding/binary"
	"errors"
	"maps"
	"math"
	"slices"
)

func byteTag(v int8) nbt.Tag { return nbt.Tag{Type: nbt.Byte, Value: v} }

//go:embed skeleton_rider_1.21.1.bin
var skeletonRiderDefaults []byte

// TrapChange describes expiration (no mounts) or activation. Mounts start with
// the existing horse, followed by the additional horses in insertion order.
// Each mount carries its passenger in the Anvil representation.
type TrapChange struct {
	Original WorldEntity
	Bolt     WorldEntity
	Mounts   []WorldEntity
}

func armedTrap(e WorldEntity) bool {
	if e.Type != SkeletonHorseEntity {
		return false
	}
	trap, _ := e.Data.Number("SkeletonTrap")
	return trap != 0
}

// Called by the owning region on load, spawn, activation and removal.
func (ch *Chunk) indexSkeletonTraps() {
	h := ch.scheduledTicks
	if h == nil {
		return
	}
	armed := false
	for i := range ch.entities {
		e := &ch.entities[i]
		if armedTrap(*e) {
			armed = true
			if e.RuntimeID == 0 {
				e.RuntimeID = NextRuntimeEntityID()
			}
		}
	}
	o := h.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	i := slices.Index(o.trapChunks, h)
	if armed && i < 0 {
		o.trapChunks = append(o.trapChunks, h)
	}
	if !armed && i >= 0 {
		o.trapChunks = slices.Delete(o.trapChunks, i, i+1)
	}
}

func (w *World) playerNearTrap(pos [3]float64) bool {
	for _, p := range w.simulationPlayers {
		x, y, z := p.X-pos[0], p.Y-pos[1], p.Z-pos[2]
		if !p.Spectator && x*x+y*y+z*z < 100 {
			return true
		}
	}
	return false
}

func (w *World) tickSkeletonTraps(eligible func(int32, int32) bool, publish func(TrapChange)) error {
	o := w.scheduledTicks
	o.mu.Lock()
	holders := slices.Clone(o.trapChunks)
	o.mu.Unlock()
	var failures []error
	for _, h := range holders {
		if !eligible(h.x, h.z) {
			continue
		}
		var changes []TrapChange
		err := w.fallingEntityOperation(h.x, h.z, func(r *Region, ch *Chunk) error {
			entities := slices.Clone(ch.entities)
			for i, original := range ch.entities {
				if !armedTrap(original) {
					continue
				}
				e := original
				e.trapTicks++
				e.Data = maps.Clone(e.Data)
				timer, _ := e.Data.Number("SkeletonTrapTime")
				noAI, _ := e.Data.Number("NoAI")
				health, hasHealth := e.Data["Health"].Value.(float32)
				checkGoals := e.trapTicks <= 1 || (e.trapTicks+e.RuntimeID)&1 == 0
				switch {
				case checkGoals && noAI == 0 && (!hasHealth || health > 0) && w.playerNearTrap(e.Position):
					change, err := w.activateSkeletonTrap(e)
					if err != nil {
						return err
					}
					entities[i] = change.Mounts[0]
					entities = append(entities, change.Mounts[1:]...)
					changes = append(changes, change)
				case timer >= 18000:
					entities[i] = WorldEntity{}
					changes = append(changes, TrapChange{Original: e})
				default:
					e.Data["SkeletonTrapTime"] = nbt.Tag{Type: nbt.Int, Value: int32(timer) + 1}
					entities[i] = e
				}
			}
			if len(changes) == 0 {
				ch.entities = entities
				ch.entitiesDirty = true
				return nil
			}
			entities = slices.DeleteFunc(entities, func(e WorldEntity) bool { return e.Type == "" })
			if w.store != nil {
				if err := persistChunkEntities(r, ch, entities); err != nil {
					return err
				}
			} else {
				ch.entities = entities
			}
			ch.indexSkeletonTraps()
			return nil
		})
		if err != nil {
			failures = append(failures, err)
			continue
		}
		for _, change := range changes {
			if change.Bolt.Type != "" {
				w.addLightning(change.Bolt, true, int64(binary.LittleEndian.Uint64(change.Bolt.UUID[:8])))
			}
			if publish != nil {
				publish(change)
			}
		}
	}
	return errors.Join(failures...)
}

func freshTrapActor(template []byte, kind string, pos [3]float64) (WorldEntity, *worldgen.LegacyRandom, error) {
	tag, _, err := nbt.ParseNetwork(template)
	if err != nil {
		return WorldEntity{}, nil, err
	}
	data, _ := tag.Object()
	id, seed, err := newLightningUUID()
	if err != nil {
		return WorldEntity{}, nil, err
	}
	rng := worldgen.NewLegacyRandom(seed)
	yaw := float32(rng.NextDouble() * float64(float32(2*math.Pi)))
	data["Rotation"] = nbt.Tag{Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: yaw}, {Type: nbt.Float, Value: float32(0)}}}
	data["Pos"] = entityPositionTag(pos)
	data["UUID"] = entityUUIDTag(id)
	data["id"] = nbt.Tag{Type: nbt.String, Value: kind}
	e, err := worldEntityFromNBT(data)
	return e, rng, err
}

func (w *World) activateSkeletonTrap(original WorldEntity) (TrapChange, error) {
	pos := original.Position
	id, seed, err := newLightningUUID()
	if err != nil {
		return TrapChange{}, err
	}
	change := TrapChange{Original: original, Bolt: WorldEntity{Type: LightningEntity, UUID: id, Position: pos, RuntimeID: NextRuntimeEntityID()}}
	random := worldgen.NewLegacyRandom(seed)
	p := worldgen.FeaturePosition{X: int32(math.Floor(pos[0])), Y: int32(math.Floor(pos[1])), Z: int32(math.Floor(pos[2]))}
	difficulty := w.localDifficulty(p)
	hard := w.baseDifficulty() == 3
	for i := 0; i < 4; i++ {
		mount := original
		if i != 0 {
			var rng *worldgen.LegacyRandom
			mount, rng, err = freshTrapActor(skeletonTrapDefaults, SkeletonHorseEntity, pos)
			if err != nil {
				return TrapChange{}, err
			}
			mount.Data["PersistenceRequired"] = byteTag(1)
			mount.RuntimeID = NextRuntimeEntityID()
			jump := float64(float32(.4)) + .2*rng.NextDouble() + .2*rng.NextDouble() + .2*rng.NextDouble()
			attr := nbt.Tag{Type: nbt.Compound, Value: nbt.Object{"id": {Type: nbt.String, Value: "minecraft:generic.jump_strength"}, "base": {Type: nbt.Double, Value: jump}}}
			mount.Data["attributes"] = mergeEntityAttributes(mount.Data["attributes"], nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{attr}})
			mount.Data["Motion"] = entityPositionTag([3]float64{1.1485 * (random.NextDouble() - random.NextDouble()), 0, 1.1485 * (random.NextDouble() - random.NextDouble())})
		} else {
			mount.Data = maps.Clone(original.Data)
			mount.Revision++
		}
		mount.Data["Tame"] = byteTag(1)
		mount.Data["SkeletonTrap"] = byteTag(0)
		mount.Data["Age"] = nbt.Tag{Type: nbt.Int, Value: int32(0)}
		rider, rng, err := freshTrapActor(skeletonRiderDefaults, "minecraft:skeleton", pos)
		if err != nil {
			return TrapChange{}, err
		}
		rider.Data["PersistenceRequired"] = byteTag(1)
		equipTrapRider(rider.Data, difficulty, hard, rng)
		mount.trapRiderID = NextRuntimeEntityID()
		passengers, _ := mount.Data["Passengers"].Elements()
		mount.Data["Passengers"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: append(slices.Clone(passengers), nbt.Tag{Type: nbt.Compound, Value: rider.Data})}
		change.Mounts = append(change.Mounts, mount)
	}
	return change, nil
}
