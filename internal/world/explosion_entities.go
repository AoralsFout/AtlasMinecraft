package world

import (
	"atlas/internal/blockrules"
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
)

const TNTEntity = "minecraft:tnt"

func ticksExplosionActor(kind string) bool {
	return kind == "minecraft:creeper" || kind == TNTEntity || kind == "minecraft:item"
}

func explosionActorChunk(pos [3]float64) chunkKey {
	return chunkKey{int32(math.Floor(pos[0])) >> 4, int32(math.Floor(pos[2])) >> 4}
}

func explosionVector(values [3]float64) nbt.Tag {
	return nbt.Tag{Type: nbt.List, ListType: nbt.Double, Value: []nbt.Tag{
		{Type: nbt.Double, Value: values[0]}, {Type: nbt.Double, Value: values[1]}, {Type: nbt.Double, Value: values[2]},
	}}
}

func shortEntityTag(value int64) nbt.Tag { return nbt.Tag{Type: nbt.Short, Value: int16(value)} }

func entityMotion(e WorldEntity) (motion [3]float64) {
	if values, ok := e.Data["Motion"].Elements(); ok && len(values) == 3 {
		for i, tag := range values {
			v, _ := tag.Value.(float64)
			if !math.IsNaN(v) && math.Abs(v) <= 10 {
				motion[i] = v
			}
		}
	}
	return motion
}

func (w *World) primeBlastTNT(p worldgen.FeaturePosition, source WorldEntity) (WorldEntity, error) {
	angle := w.tickRandom.NextDouble() * float64(float32(2*math.Pi))
	fuse := int64(10 + w.tickRandom.NextIntBound(20))
	// Entity UUID generation has its own RNG. Only launch angle and fuse use the level RNG.
	hi, lo := rand.Uint64()&0xffffffffffff0fff|0x4000, rand.Uint64()&0x3fffffffffffffff|0x8000000000000000
	pos := [3]float64{float64(p.X) + .5, float64(p.Y), float64(p.Z) + .5}
	motion := [3]float64{-math.Sin(angle) * .02, float64(float32(.2)), -math.Cos(angle) * .02}
	e, err := worldEntityFromNBT(nbt.Object{
		"id":       {Type: nbt.String, Value: TNTEntity},
		"UUID":     {Type: nbt.IntArray, Value: []int32{int32(hi >> 32), int32(hi), int32(lo >> 32), int32(lo)}},
		"Pos":      explosionVector(pos),
		"Motion":   explosionVector(motion),
		"Rotation": {Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: float32(0)}, {Type: nbt.Float, Value: float32(0)}}},
		"fuse":     shortEntityTag(fuse),
		"Air":      shortEntityTag(300),
		"Fire":     shortEntityTag(-1),
		"OnGround": byteTag(0),
		"block_state": {Type: nbt.Compound, Value: nbt.Object{
			"Name":       {Type: nbt.String, Value: TNTEntity},
			"Properties": {Type: nbt.Compound, Value: nbt.Object{"unstable": {Type: nbt.String, Value: "false"}}},
		}},
	})
	e.RuntimeID = NextRuntimeEntityID()
	e.tntOwner = source.RuntimeID
	if source.Type == TNTEntity {
		e.tntOwner = source.tntOwner
	}
	return e, err
}

func (w *World) tickExplosionActors(eligible func(int32, int32) bool, publish func(LivingDamageChange)) error {
	type entry struct {
		key    chunkKey
		entity WorldEntity
	}
	o := w.scheduledTicks
	o.mu.Lock()
	holders := slices.Clone(o.explosionChunks)
	o.mu.Unlock()
	var pending []entry
	for _, h := range holders {
		if !eligible(h.x, h.z) {
			continue
		}
		key := chunkKey{h.x, h.z}
		if err := w.fallingEntityOperation(key.x, key.z, func(_ *Region, ch *Chunk) error {
			for _, e := range ch.entities {
				if ticksExplosionActor(e.Type) {
					pending = append(pending, entry{key, e})
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if len(pending) == 0 {
		return nil
	}
	// The active list is snapshotted before any entity ticks. Entities spawned by
	// an explosion first tick on the following pass, even in a different chunk.
	slices.SortFunc(pending, func(a, b entry) int { return cmp.Compare(a.entity.RuntimeID, b.entity.RuntimeID) })
	peaceful := w.baseDifficulty() == 0
	var failure error
	for _, actor := range pending {
		if actor.entity.Type == "minecraft:creeper" {
			hp, ok := actor.entity.Data["Health"].Value.(float32)
			if !ok || hp > 0 {
				failure = errors.Join(failure, w.tickCreeper(actor.key, actor.entity, peaceful, publish))
			}
			continue
		}
		// An earlier blast in this same pass may have pushed or removed the actor.
		var current WorldEntity
		if err := w.fallingEntityOperation(actor.key.x, actor.key.z, func(_ *Region, ch *Chunk) error {
			for _, e := range ch.entities {
				if e.UUID == actor.entity.UUID {
					current = e
					break
				}
			}
			return nil
		}); err != nil {
			failure = errors.Join(failure, err)
			continue
		}
		if current.Type != "" {
			if err := w.tickBlastActor(actor.key, current, publish); err != nil {
				failure = errors.Join(failure, fmt.Errorf("tick %s %x: %w", current.Type, current.UUID, err))
			}
		}
	}
	return failure
}

func (w *World) tickBlastActor(key chunkKey, before WorldEntity, publish func(LivingDamageChange)) error {
	e := before
	e.Data = maps.Clone(e.Data)
	motion := entityMotion(e)
	isTNT := e.Type == TNTEntity
	size := .25
	if isTNT {
		size = float64(float32(.98))
	}
	if gravity, _ := e.Data.Number("NoGravity"); gravity == 0 {
		motion[1] -= .04
	}
	grounded, err := w.moveBlastActor(&e, &motion, size)
	if err != nil {
		return err
	}
	e.Data["OnGround"] = byteTag(0)
	if grounded {
		e.Data["OnGround"] = byteTag(1)
	}
	friction := .98
	if !isTNT {
		friction = float64(float32(.98))
		if grounded {
			friction, err = w.itemGroundFriction(e.Position)
			if err != nil {
				return err
			}
		}
	}
	motion[0] *= friction
	motion[1] *= .98
	motion[2] *= friction
	if grounded {
		if isTNT {
			motion[0] *= .7
			motion[2] *= .7
		}
		if isTNT || motion[1] < 0 {
			motion[1] *= -.5
		}
	}
	e.Data["Pos"] = explosionVector(e.Position)
	e.Data["Motion"] = explosionVector(motion)
	e.Revision++
	removed := e.Position[1] < float64(w.MinBuildHeight()-64)
	change := LivingDamageChange{Entity: e}
	if isTNT {
		fuse, _ := e.Data.Number("fuse")
		e.Data["fuse"] = shortEntityTag(fuse - 1)
		if fuse <= 1 {
			change.Explosion = &ExplosionEvent{Center: e.Position, Power: 4}
			change.Explosion.Center[1] += size * .0625
			removed = true
		}
	} else {
		age, _ := e.Data.Number("Age")
		if age != -32768 {
			age++
			e.Data["Age"] = shortEntityTag(age)
		}
		if delay, _ := e.Data.Number("PickupDelay"); delay > 0 && delay != 32767 {
			e.Data["PickupDelay"] = shortEntityTag(delay - 1)
		}
		stack, _ := e.Data.Object("Item")
		count, _ := stack.Number("count")
		removed = removed || age >= 6000 || count <= 0
	}
	change.Removed = removed
	change.Moved = isTNT || before.Position != e.Position || entityMotion(before) != motion
	if err := w.storeBlastActor(key, e, removed, change.Explosion != nil); err != nil {
		return err
	}
	if change.Explosion != nil {
		err = w.explode(e, change.Explosion, publish)
	}
	if publish != nil && (removed || change.Moved) {
		publish(change)
	}
	return err
}

func (w *World) moveBlastActor(e *WorldEntity, velocity *[3]float64, size float64) (bool, error) {
	x, y, z := e.Position[0], e.Position[1], e.Position[2]
	half := size / 2
	box := blockrules.Box{x - half, y, z - half, x + half, y + size, z + half}
	var bounds blockrules.Box
	for i, d := range velocity {
		bounds[i] = box[i] + min(0, d)
		bounds[i+3] = box[i+3] + max(0, d)
	}
	obstacles, err := w.collisionBoxes(bounds)
	if errors.Is(err, ErrChunkNotLoaded) {
		for cx := int32(math.Floor(bounds[0])) >> 4; cx <= int32(math.Floor(bounds[3]))>>4; cx++ {
			for cz := int32(math.Floor(bounds[2])) >> 4; cz <= int32(math.Floor(bounds[5]))>>4; cz++ {
				if _, err := w.LoadChunk(cx, cz); err != nil {
					return false, err
				}
			}
		}
		obstacles, err = w.collisionBoxes(bounds)
	}
	if err != nil {
		return false, err
	}
	delta := worldgen.ClipEntityMovement(box, *velocity, obstacles)
	if delta[0]*delta[0]+delta[1]*delta[1]+delta[2]*delta[2] > 1e-7 {
		for i := range delta {
			e.Position[i] += delta[i]
		}
	}
	grounded := (*velocity)[1] < 0 && (*velocity)[1] != delta[1]
	for i, v := range *velocity {
		if i == 1 && v != delta[i] || i != 1 && math.Abs(v-delta[i]) >= 1e-5 {
			(*velocity)[i] = 0
		}
	}
	return grounded, nil
}

func (w *World) itemGroundFriction(pos [3]float64) (float64, error) {
	p := BlockPosition{int32(math.Floor(pos[0])), int32(math.Floor(pos[1] - .500001)), int32(math.Floor(pos[2]))}
	state, _, err := w.loadedState(p)
	if err != nil {
		return 0, err
	}
	friction := float32(.6)
	switch state.Name {
	case "minecraft:ice", "minecraft:packed_ice", "minecraft:frosted_ice":
		friction = .98
	case "minecraft:blue_ice":
		friction = .989
	case "minecraft:slime_block":
		friction = .8
	}
	return float64(friction * float32(.98)), nil
}

// The world tick holds blockUpdateMu across both sides of a chunk move. Save
// and unload can only see the completed move. Ordinary motion uses dirty saves.
func (w *World) storeBlastActor(key chunkKey, e WorldEntity, removed, persist bool) error {
	dest := explosionActorChunk(e.Position)
	moved := !removed && dest != key
	removeAt := func(at chunkKey, save bool) error {
		return w.fallingEntityOperation(at.x, at.z, func(actor *Region, ch *Chunk) error {
			i := slices.IndexFunc(ch.entities, func(old WorldEntity) bool { return old.UUID == e.UUID })
			if i < 0 {
				return nil
			}
			updated := slices.Delete(slices.Clone(ch.entities), i, i+1)
			if save && w.store != nil {
				return persistChunkEntities(actor, ch, updated)
			}
			ch.entities, ch.entitiesDirty = updated, true
			ch.indexLivingDamage()
			return nil
		})
	}
	if removed {
		return removeAt(key, persist)
	}
	if moved {
		if err := w.fallingEntityOperation(dest.x, dest.z, func(_ *Region, ch *Chunk) error {
			ch.entities = append(ch.entities, e)
			ch.entitiesDirty = true
			ch.indexLivingDamage()
			return nil
		}); err != nil {
			return err
		}
		if err := removeAt(key, false); err != nil {
			return errors.Join(err, removeAt(dest, false))
		}
		return nil
	}
	return w.fallingEntityOperation(key.x, key.z, func(_ *Region, ch *Chunk) error {
		for i, old := range ch.entities {
			if old.UUID == e.UUID {
				ch.entities[i], ch.entitiesDirty = e, true
				break
			}
		}
		return nil
	})
}

func (w *World) blastItemsAndTNT(chunks []*chunkScheduledTicks, source WorldEntity, center [3]float64, radius float64,
	impact func([3]float64, float64, float64) (float64, bool, error), publish func(LivingDamageChange)) error {
	for _, h := range chunks {
		key := chunkKey{h.x, h.z}
		var targets []WorldEntity
		if err := w.fallingEntityOperation(key.x, key.z, func(_ *Region, ch *Chunk) error {
			for _, e := range ch.entities {
				if (e.Type == TNTEntity || e.Type == "minecraft:item") && e.UUID != source.UUID {
					targets = append(targets, e)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		for _, e := range targets {
			size, eye := .25, float64(float32(.2125))
			if e.Type == TNTEntity {
				size, eye = float64(float32(.98)), 0
			}
			direction := [3]float64{e.Position[0] - center[0], e.Position[1] + eye - center[1], e.Position[2] - center[2]}
			length := math.Sqrt(direction[0]*direction[0] + direction[1]*direction[1] + direction[2]*direction[2])
			if length == 0 {
				continue
			}
			strength, hit, err := impact(e.Position, size, size)
			if err != nil {
				return err
			}
			if !hit {
				continue
			}
			e.Data = maps.Clone(e.Data)
			removed := false
			if e.Type == "minecraft:item" {
				stack, _ := e.Data.Object("Item")
				name, _ := stack.Text("id")
				if invulnerable, _ := e.Data.Number("Invulnerable"); invulnerable == 0 && name != "minecraft:nether_star" {
					health, ok := e.Data.Number("Health")
					if !ok {
						health = 5
					}
					damage := float32((strength*strength+strength)/2*7*radius + 1)
					health = int64(float32(health) - damage)
					e.Data["Health"] = shortEntityTag(health)
					removed = health <= 0
				}
			}
			motion := entityMotion(e)
			for i := range motion {
				motion[i] += direction[i] / length * strength
			}
			e.Data["Motion"] = explosionVector(motion)
			e.Revision++
			if err := w.storeBlastActor(key, e, removed, false); err != nil {
				return err
			}
			if publish != nil {
				publish(LivingDamageChange{Entity: e, Removed: removed, Moved: strength != 0})
			}
		}
	}
	return nil
}
