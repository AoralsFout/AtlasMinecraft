package world

import (
	"atlas/internal/worldgen"
	_ "embed"
	"encoding/json"
)

//go:embed copper_weathering_1.21.1.json
var copperWeatheringJSON []byte

type copperStages struct {
	First    string
	Previous string
}

// Only registered WeatheringCopper blocks participate, including fresh copper.
var copperWeathering = func() map[string]copperStages {
	var blocks map[string]copperStages
	if err := json.Unmarshal(copperWeatheringJSON, &blocks); err != nil {
		panic(err)
	}
	return blocks
}()

func (r *fluidRuntime) cleanLightningCopper(strike worldgen.FeaturePosition) error {
	origin := strike
	state, err := r.BlockAt(origin)
	if err != nil {
		return err
	}
	if state.Name == "minecraft:lightning_rod" {
		origin = fluidAdjacent(strike, oppositeFace(FacingFace(state.Properties["facing"])))
		state, err = r.BlockAt(origin)
		if err != nil {
			return err
		}
	}
	stages, copper := copperWeathering[state.Name]
	if !copper {
		return nil
	}
	if err := r.world.ensureTickRandom(); err != nil {
		return err
	}
	// The four stages of a family share their properties (including both door halves).
	if state.Name != stages.First {
		state.Name = stages.First
		if err := r.SetFluidBlock(origin, state, 3); err != nil {
			return err
		}
	}
	return walkLightningCopper(origin, r.world.tickRandom, r.cleanCopperStep)
}

func (r *fluidRuntime) cleanCopperStep(p worldgen.FeaturePosition) (bool, error) {
	state, err := r.BlockAt(p)
	if err != nil {
		return false, err
	}
	stages, ok := copperWeathering[state.Name]
	if !ok {
		return false, nil
	}
	if stages.Previous != "" {
		state.Name = stages.Previous
		if err := r.SetFluidBlock(p, state, 3); err != nil {
			return false, err
		}
	}
	return true, r.FluidLevelEvent(p, 3002, -1)
}

// Every walk restarts at the cleaned block. A failed step ends just that walk.
func walkLightningCopper(origin worldgen.FeaturePosition, random *worldgen.LegacyRandom, visit func(worldgen.FeaturePosition) (bool, error)) error {
	walks := 3 + random.NextIntBound(3)
	for range walks {
		p := origin
		steps := 1 + random.NextIntBound(8)
		for range steps {
			found := false
			for range 10 {
				q := p
				q.X += random.NextIntBound(3) - 1
				q.Y += random.NextIntBound(3) - 1
				q.Z += random.NextIntBound(3) - 1
				ok, err := visit(q)
				if err != nil {
					return err
				}
				if ok {
					p = q
					found = true
					break
				}
			}
			if !found {
				break
			}
		}
	}
	return nil
}
