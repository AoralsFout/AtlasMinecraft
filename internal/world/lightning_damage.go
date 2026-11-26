package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"errors"
	"maps"
	"math"
	"slices"
)

// The damage cooldown and last incoming amount are runtime-only in LivingEntity.
// HurtTime is the shorter, independently saved animation timer.
type hurtCooldown struct {
	ticks   int
	amount  float32
	burning bool
}

type LivingDamageChange struct {
	Entity           WorldEntity
	Source           string
	Removed          bool
	Spawned          bool
	MooshroomVariant string
	SoundSeed        int64
}
type lightningImpact struct {
	position      [3]float64
	peaceful      bool
	uuid          [16]byte
	lastOrder     uint64
	random        *worldgen.LegacyRandom
	nextSoundSeed func() int64
}

func livingSize(e WorldEntity) (width, height float64) {
	size := entitySizes[e.Type]
	age, _ := e.Data.Number("Age")
	baby, _ := e.Data.Number("IsBaby")
	scale := float32(max(.0625, min(16, livingAttribute(e, "minecraft:generic.scale", 1))))
	if age < 0 || baby != 0 {
		scale *= .5
	}
	return float64(size[0] * scale), float64(size[1] * scale)
}

func lightningIntersects(pos [3]float64, e WorldEntity) bool {
	width, height := livingSize(e)
	x, y, z := e.Position[0], e.Position[1], e.Position[2]
	return x+width/2 > pos[0]-3 && x-width/2 < pos[0]+3 &&
		y+height > pos[1]-3 && y < pos[1]+9 && z+width/2 > pos[2]-3 && z-width/2 < pos[2]+3
}

func hasLivingTimers(data nbt.Object, traits livingTrait) bool {
	if hp, ok := data["Health"].Value.(float32); ok && hp <= 0 {
		return true
	}
	fire, _ := data.Number("Fire")
	if fire > 0 || (traits.FireImmune && fire < 0) {
		return true
	}
	for _, key := range []string{"HurtTime", "DeathTime"} {
		if n, _ := data.Number(key); n > 0 {
			return true
		}
	}
	return false
}

func (ch *Chunk) indexLivingDamage() {
	h := ch.scheduledTicks
	if h == nil {
		return
	}
	living, active, exposed := false, len(ch.hurtCooldowns) > 0, false
	var inspect func(string, nbt.Object)
	inspect = func(kind string, d nbt.Object) {
		if traits, ok := livingDefaults.Mobs[kind]; ok {
			living = true
			active = active || hasLivingTimers(d, traits)
			fire, _ := d.Number("Fire")
			exposed = exposed || fire < 0
		}
		passengers, _ := d["Passengers"].Elements()
		for _, tag := range passengers {
			child, _ := tag.Object()
			id, _ := child.Text("id")
			inspect(id, child)
		}
	}
	for _, e := range ch.entities {
		inspect(e.Type, e.Data)
	}
	o := h.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	h.hasLiving = living
	h.canExtinguish = exposed
	if !active {
		delete(o.hurtChunks, h)
		return
	}
	if o.hurtChunks == nil {
		o.hurtChunks = make(map[*chunkScheduledTicks]struct{})
	}
	o.hurtChunks[h] = struct{}{}
}

func (w *World) strikeLivingEntities(bolt lightningImpact, publish func(LivingDamageChange)) error {
	if err := w.ensureTickRandom(); err != nil {
		return err
	}
	bolt.random = w.tickRandom
	bolt.nextSoundSeed = w.nextSoundSeed
	o := w.scheduledTicks
	o.mu.Lock()
	var targets []*chunkScheduledTicks
	for _, h := range o.loadOrder {
		if h.hasLiving {
			targets = append(targets, h)
		}
	}
	o.mu.Unlock()
	for _, h := range targets {
		if err := w.updateLivingChunk(h, &bolt, nil, nil, publish); err != nil {
			return err
		}
	}
	return nil
}

func (w *World) tickLivingDamage(eligible func(int32, int32) bool, publish func(LivingDamageChange), deferred map[uint64][]func() error) error {
	o := w.scheduledTicks
	checkExposure := w.WeatherLevels().IsRaining() || len(w.bolts) > 0
	o.mu.Lock()
	holders := slices.Collect(maps.Keys(o.hurtChunks))
	if checkExposure {
		for _, h := range o.loadOrder {
			if _, active := o.hurtChunks[h]; h.canExtinguish && !active {
				holders = append(holders, h)
			}
		}
	}
	o.mu.Unlock()
	for _, h := range holders {
		if !eligible(h.x, h.z) {
			continue
		}
		// Rain/fluids may require reads from a neighboring region. Resolve them
		// outside the owning actor, before applying the tick to its entities.
		var burning []WorldEntity
		later := make(map[uint64]bool)
		var tickAfter map[[16]byte]uint64
		err := w.fallingEntityOperation(h.x, h.z, func(_ *Region, ch *Chunk) error {
			for _, e := range ch.entities {
				// A replacement first ticks on the following world tick, after
				// every surviving bolt that was already in the entity list.
				for i := len(w.bolts) - 1; e.afterBolt != 0 && i >= 0; i-- {
					order := w.bolts[i].order
					if order > e.afterBolt {
						continue
					}
					if tickAfter == nil {
						tickAfter = make(map[[16]byte]uint64)
					}
					tickAfter[e.UUID] = order
					later[order] = true
					break
				}
				if err := visitBurning(e, checkExposure, &burning); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		wet := make(map[[16]byte]bool, len(burning))
		for _, e := range burning {
			value, err := w.livingWet(e)
			if err != nil {
				return err
			}
			wet[e.UUID] = value
		}
		var now func([16]byte) bool
		if len(tickAfter) > 0 {
			now = func(id [16]byte) bool { return tickAfter[id] == 0 }
		}
		if err := w.updateLivingChunk(h, nil, wet, now, publish); err != nil {
			return err
		}
		for order := range later {
			deferred[order] = append(deferred[order], func() error {
				return w.updateLivingChunk(h, nil, wet, func(id [16]byte) bool { return tickAfter[id] == order }, publish)
			})
		}
	}
	return nil
}

func visitBurning(e WorldEntity, checkExposure bool, out *[]WorldEntity) error {
	if _, ok := livingDefaults.Mobs[e.Type]; ok {
		if fire, _ := e.Data.Number("Fire"); fire > 0 || (checkExposure && fire < 0) {
			*out = append(*out, e)
		}
	}
	tags, _ := e.Data["Passengers"].Elements()
	for _, tag := range tags {
		d, _ := tag.Object()
		child, err := worldEntityFromNBT(d)
		if err != nil {
			return err
		}
		if err := visitBurning(child, checkExposure, out); err != nil {
			return err
		}
	}
	return nil
}

func (w *World) livingWet(e WorldEntity) (bool, error) {
	r := fluidRuntime{world: w}
	x, y, z := e.Position[0], e.Position[1], e.Position[2]
	width, height := livingSize(e)
	p := worldgen.FeaturePosition{X: int32(math.Floor(x)), Y: int32(math.Floor(y)), Z: int32(math.Floor(z))}
	if w.WeatherLevels().IsRaining() {
		for _, at := range []int32{p.Y, int32(math.Floor(y + height))} {
			p.Y = at
			wet, err := r.rainingAt(p)
			if wet || err != nil {
				return wet, err
			}
		}
	}
	// Entity.updateFluidHeightAndDoFluidPushing checks the deflated hitbox.
	for bx := int32(math.Floor(x - width/2 + .001)); bx < int32(math.Ceil(x+width/2-.001)); bx++ {
		for bz := int32(math.Floor(z - width/2 + .001)); bz < int32(math.Ceil(z+width/2-.001)); bz++ {
			for by := int32(math.Floor(y + .001)); by < int32(math.Ceil(y+height-.001)); by++ {
				s, err := r.BlockAt(worldgen.FeaturePosition{X: bx, Y: by, Z: bz})
				if errors.Is(err, ErrChunkNotLoaded) {
					continue
				}
				if err != nil {
					return false, err
				}
				if s.Name == "minecraft:powder_snow" || s.Name == "minecraft:bubble_column" {
					return true, nil
				}
				f, err := worldgen.FluidStateForBlock(s)
				if err != nil {
					return false, err
				}
				if (f.Type == worldgen.FluidWater || f.Type == worldgen.FluidFlowingWater) && float64(by)+float64(f.Amount)/9 >= y+.001 {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (w *World) updateLivingChunk(h *chunkScheduledTicks, bolt *lightningImpact, wet map[[16]byte]bool, selected func([16]byte) bool, publish func(LivingDamageChange)) error {
	var changes []LivingDamageChange
	err := w.fallingEntityOperation(h.x, h.z, func(_ *Region, ch *Chunk) error {
		var dismounted []WorldEntity
		var discarded bool
		for i := 0; i < len(ch.entities); {
			e := &ch.entities[i]
			if selected != nil && !selected(e.UUID) {
				i++
				continue
			}
			changed, removed, err := ch.updateLivingTree(e, bolt, wet, &changes, &dismounted)
			if err != nil {
				return err
			}
			ch.entitiesDirty = ch.entitiesDirty || changed
			if removed {
				discarded = true
				ch.entities = slices.Delete(ch.entities, i, i+1)
				continue
			}
			i++
		}
		ch.entities = append(ch.entities, dismounted...)
		if discarded {
			ch.indexSkeletonTraps()
		}
		ch.indexLivingDamage()
		return nil
	})
	if err != nil {
		return err
	}
	if publish != nil {
		for _, change := range changes {
			publish(change)
		}
	}
	return nil
}

func (ch *Chunk) updateLivingTree(e *WorldEntity, bolt *lightningImpact, wet map[[16]byte]bool, changes *[]LivingDamageChange, dismounted *[]WorldEntity) (bool, bool, error) {
	changed, removed := false, false
	if traits, ok := livingDefaults.Mobs[e.Type]; ok {
		before := *e
		cooldown := ch.hurtCooldowns[e.UUID]
		wasBurning := cooldown.burning
		source := ""
		if bolt != nil {
			hp, _ := e.Data["Health"].Value.(float32)
			if hp > 0 && lightningIntersects(bolt.position, *e) {
				switch {
				case e.Type == "minecraft:mooshroom":
					changed = ch.convertMooshroom(e, bolt, changes)
				case !bolt.peaceful && (e.Type == "minecraft:pig" || e.Type == "minecraft:villager"):
					replacement, err := lightningReplacement(*e, bolt)
					if err != nil {
						return false, false, err
					}
					*dismounted = append(*dismounted, replacement)
					*changes = append(*changes, LivingDamageChange{Entity: replacement, Spawned: true})
					changed, removed = true, true
				default:
					changed, source = thunderHit(e, traits, &cooldown)
				}
			}
		} else if cooldown.ticks > 0 || cooldown.burning || hasLivingTimers(e.Data, traits) || wet[e.UUID] {
			changed, removed, source = advanceLivingDamage(e, traits, &cooldown, wet[e.UUID])
		}
		flagChanged := wasBurning != cooldown.burning
		e.OnFire = cooldown.burning
		if cooldown.ticks > 0 || cooldown.burning {
			if ch.hurtCooldowns == nil {
				ch.hurtCooldowns = make(map[[16]byte]hurtCooldown)
			}
			ch.hurtCooldowns[e.UUID] = cooldown
		} else {
			delete(ch.hurtCooldowns, e.UUID)
		}
		if removed {
			delete(ch.hurtCooldowns, e.UUID)
			delete(ch.lastMooshroomBolt, e.UUID)
		}
		if changed || flagChanged {
			e.Revision++
			if removed || source != "" || flagChanged || before.Data["Health"].Value != e.Data["Health"].Value {
				*changes = append(*changes, LivingDamageChange{Entity: *e, Source: source, Removed: removed})
			}
		}
	}
	tags, _ := e.Data["Passengers"].Elements()
	var updated []nbt.Tag
	for i, tag := range tags {
		data, _ := tag.Object()
		child, err := worldEntityFromNBT(data)
		if err != nil {
			return changed, removed, err
		}
		child.Revision = e.Revision
		dirty, discard, err := ch.updateLivingTree(&child, bolt, wet, changes, dismounted)
		if err != nil {
			return changed, removed, err
		}
		if !dirty && updated == nil {
			continue
		}
		if updated == nil {
			updated = append(make([]nbt.Tag, 0, len(tags)), tags[:i]...)
		}
		if !discard {
			updated = append(updated, nbt.Tag{Type: nbt.Compound, Value: child.Data})
		}
	}
	if updated != nil {
		e.Data = maps.Clone(e.Data)
		if len(updated) == 0 {
			delete(e.Data, "Passengers")
		} else {
			e.Data["Passengers"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: updated}
		}
		e.Revision++
		changed = true
	}
	if removed {
		riders, err := EntityPassengers(*e)
		if err != nil {
			return changed, removed, err
		}
		for i := range riders {
			riders[i].OnFire = ch.hurtCooldowns[riders[i].UUID].burning
		}
		*dismounted = append(*dismounted, riders...)
	}
	return changed, removed, nil
}
