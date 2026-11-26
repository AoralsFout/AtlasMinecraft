package world

import (
	"atlas/internal/worldgen"
	"errors"
	"math"
	"strconv"
)

func (w *World) FireTick() bool {
	w = w.clockOwner()
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return !w.fireTickStopped
}

func (w *World) SetFireTick(enabled bool) error {
	return w.setCycleRule("doFireTick", strconv.FormatBool(enabled), func(w *World) { w.fireTickStopped = !enabled })
}

// The first flash samples four offsets in addition to the strike position.
// Later flashes retry just the strike position, even on Peaceful and Easy.
func (r *fluidRuntime) lightningFire(bolt lightningBolt, offsets int) error {
	if bolt.visualOnly || !r.world.FireTick() {
		return nil
	}
	pos := bolt.entity.Position
	center := worldgen.FeaturePosition{X: int32(math.Floor(pos[0])), Y: int32(math.Floor(pos[1])), Z: int32(math.Floor(pos[2]))}
	var failure error
	for i := 0; i <= offsets; i++ {
		p := center
		if i > 0 {
			p.X += bolt.random.NextIntBound(3) - 1
			p.Y += bolt.random.NextIntBound(3) - 1
			p.Z += bolt.random.NextIntBound(3) - 1
		}
		if err := r.igniteAir(p); err != nil {
			failure = errors.Join(failure, err)
		}
	}
	return failure
}

func (r *fluidRuntime) igniteAir(p worldgen.FeaturePosition) error {
	if !r.InWorld(p) {
		return nil
	}
	state, err := r.BlockAt(p)
	if err != nil || !fireAir(state) {
		return err
	}
	fire, valid, err := worldgen.FirePlacement(r, p)
	if err != nil || !valid {
		return err
	}
	return r.SetFluidBlock(p, fire, 3)
}
