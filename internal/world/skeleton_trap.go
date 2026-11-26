package world

import (
	"atlas/internal/tickqueue"
	"atlas/internal/worldgen"
	_ "embed"
	"strconv"
)

const SkeletonHorseEntity = "minecraft:skeleton_horse"

// Constructor NBT captured from a horse created by ServerLevel.tickChunk.
//
//go:embed skeleton_trap_1.21.1.bin
var skeletonTrapDefaults []byte

func (w *World) MobSpawning() bool {
	w = w.clockOwner()
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return !w.mobsDisabled
}

func (w *World) SetMobSpawning(enabled bool) error {
	return w.setCycleRule("doMobSpawning", strconv.FormatBool(enabled), func(owner *World) { owner.mobsDisabled = !enabled })
}

func (w *World) baseDifficulty() int64 {
	base := int64(2)
	if w.store != nil {
		v := w.store.vanilla
		v.mu.Lock()
		root, _ := v.root.Object()
		data, _ := root.Object("Data")
		if id, ok := data.Number("Difficulty"); ok {
			base = id
		}
		v.mu.Unlock()
	}
	return base
}

func (w *World) localDifficulty(p worldgen.FeaturePosition) float32 {
	base := w.baseDifficulty()
	if base == 0 {
		return 0
	}
	_, day := w.Time()
	global := min(max((float32(day)-72000)/1440000, 0), 1) * 0.25
	var inhabited int64
	var moon float32
	o := w.scheduledTicks
	o.mu.Lock()
	if h := o.chunks[tickqueue.ChunkKey(p.X>>4, p.Z>>4)]; h != nil {
		inhabited = h.inhabitedTime
		moon = [8]float32{1, .75, .5, .25, 0, .25, .5, .75}[(day/24000)&7]
	}
	o.mu.Unlock()
	local := min(max(float32(inhabited)/3600000, 0), 1)
	if base != 3 {
		local *= 0.75
	}
	local += min(moon*0.25, global)
	if base == 1 {
		local *= 0.5
	}
	return float32(base) * ((0.75 + global) + local)
}

func (r *fluidRuntime) spawnSkeletonTrap(p worldgen.FeaturePosition) (*WorldEntity, error) {
	horse, _, err := freshTrapActor(skeletonTrapDefaults, SkeletonHorseEntity, [3]float64{float64(p.X), float64(p.Y), float64(p.Z)})
	if err != nil {
		return nil, err
	}
	horse.RuntimeID = NextRuntimeEntityID()
	err = r.world.fallingEntityOperation(p.X>>4, p.Z>>4, func(actor *Region, ch *Chunk) error {
		if r.world.store == nil {
			ch.entities = append(ch.entities, horse)
			ch.indexSkeletonTraps()
			return nil
		}
		if err := persistChunkEntities(actor, ch, append(ch.entities, horse)); err != nil {
			return err
		}
		ch.indexSkeletonTraps()
		return nil
	})
	return &horse, err
}
