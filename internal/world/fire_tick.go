package world

import (
	"atlas/internal/worldgen"
	_ "embed"
	"encoding/json"
	"strconv"
)

//go:embed fire_odds_1.21.1.json
var fireOddsJSON []byte

// FireBlock.getIgniteOdds and getBurnOdds for the default state of each fuel.
var fireOdds = func() map[string][2]int32 {
	var odds map[string][2]int32
	if err := json.Unmarshal(fireOddsJSON, &odds); err != nil {
		panic(err)
	}
	return odds
}()

func fireAir(s worldgen.SurfaceBlockState) bool {
	return s.Name == "minecraft:air" || s.Name == "minecraft:cave_air" || s.Name == "minecraft:void_air"
}

func (r *fluidRuntime) fuelAt(p worldgen.FeaturePosition) ([2]int32, error) {
	s, err := r.BlockAt(p)
	if err != nil || s.Properties["waterlogged"] == "true" {
		return [2]int32{}, err
	}
	return fireOdds[s.Name], nil
}

func (r *fluidRuntime) scheduleFire(p worldgen.FeaturePosition) error {
	delay, err := r.ShapeRandomInt(10)
	if err != nil {
		return err
	}
	return r.ScheduleBlockTick(p, "minecraft:fire", 30+delay, 0)
}

func (r *fluidRuntime) onFirePlaced(p worldgen.FeaturePosition, s worldgen.SurfaceBlockState) error {
	dim := r.world.DimensionName()
	formedPortal := false
	if dim == "minecraft:overworld" || dim == "minecraft:the_nether" {
		shape, found, err := worldgen.FindEmptyNetherPortalShape(r, r.MinBuildHeight(), p, "x")
		if err != nil {
			return err
		}
		if found {
			if err := worldgen.FillNetherPortalShape(r, shape); err != nil {
				return err
			}
			formedPortal = true
		}
	}
	if !formedPortal {
		placement, supported, err := worldgen.FirePlacement(r, p)
		if err != nil {
			return err
		}
		if !supported || (s.Name == "minecraft:soul_fire" && placement.Name != s.Name) {
			if err := r.removeFireBlock(p); err != nil {
				return err
			}
		}
	}
	if s.Name == "minecraft:fire" {
		return r.scheduleFire(p)
	}
	return nil
}

func (r *fluidRuntime) nearRain(p worldgen.FeaturePosition) (bool, error) {
	if !r.world.WeatherLevels().IsRaining() {
		return false, nil
	}
	for _, q := range []worldgen.FeaturePosition{p, fluidAdjacent(p, 4), fluidAdjacent(p, 5), fluidAdjacent(p, 2), fluidAdjacent(p, 3)} {
		if rain, err := r.rainingAt(q); err != nil || rain {
			return rain, err
		}
	}
	return false, nil
}

func (r *fluidRuntime) removeFireBlock(p worldgen.FeaturePosition) error {
	return r.SetFluidBlock(p, worldgen.SurfaceBlockState{Name: "minecraft:air"}, 3)
}

func (r *fluidRuntime) setFireWithAge(p worldgen.FeaturePosition, age int32) error {
	s, _, err := worldgen.FirePlacement(r, p)
	if err != nil {
		return err
	}
	if s.Name == "minecraft:fire" {
		if s.Properties == nil {
			s.Properties = make(map[string]string)
		}
		s.Properties["age"] = strconv.Itoa(int(min(int32(15), age)))
	}
	return r.SetFluidBlock(p, s, 3)
}

func (r *fluidRuntime) maxIgniteOdds(p worldgen.FeaturePosition) (int32, error) {
	var odds int32
	for face := 0; face < 6; face++ {
		fuel, err := r.fuelAt(fluidAdjacent(p, face))
		if err != nil {
			return 0, err
		}
		odds = max(odds, fuel[0])
	}
	return odds, nil
}

func (r *fluidRuntime) burnNeighbor(p worldgen.FeaturePosition, bound, age int32) error {
	fuel, err := r.fuelAt(p)
	if err != nil {
		return err
	}
	rng := r.world.tickRandom
	if rng.NextIntBound(bound) >= fuel[1] {
		return nil
	}
	if rng.NextIntBound(age+10) < 5 {
		rain, err := r.rainingAt(p)
		if err != nil {
			return err
		}
		if !rain {
			return r.setFireWithAge(p, age+rng.NextIntBound(5)/4)
		}
	}
	return r.removeFireBlock(p)
}

func (r *fluidRuntime) spreadFire(p worldgen.FeaturePosition, age int32, humid bool) error {
	offset := int32(0)
	if humid {
		offset = -50
	}
	for _, face := range []int{5, 4, 0, 1, 2, 3} {
		bound := int32(300)
		if face < 2 {
			bound = 250
		}
		if err := r.burnNeighbor(fluidAdjacent(p, face), bound+offset, age); err != nil {
			return err
		}
	}
	base := int32(r.world.baseDifficulty())
	rng := r.world.tickRandom
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			for y := int32(-1); y <= 4; y++ {
				if x == 0 && y == 0 && z == 0 {
					continue
				}
				q := worldgen.FeaturePosition{X: p.X + x, Y: p.Y + y, Z: p.Z + z}
				s, err := r.BlockAt(q)
				if err != nil {
					return err
				}
				if !fireAir(s) {
					continue
				}
				odds, err := r.maxIgniteOdds(q)
				if err != nil {
					return err
				}
				if odds == 0 {
					continue
				}
				chance := (odds + 40 + base*7) / (age + 30)
				if humid {
					chance /= 2
				}
				if chance > 0 && rng.NextIntBound(100*max(int32(1), y)) <= chance {
					wet, err := r.nearRain(q)
					if err != nil {
						return err
					}
					if !wet {
						if err := r.setFireWithAge(q, age+rng.NextIntBound(5)/4); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func (r *fluidRuntime) TickFire(p worldgen.FeaturePosition, s worldgen.SurfaceBlockState) error {
	if err := r.scheduleFire(p); err != nil {
		return err
	}
	if !r.world.FireTick() {
		return nil
	}
	_, valid, err := worldgen.FirePlacement(r, p)
	if err != nil {
		return err
	}
	if !valid {
		if err := r.removeFireBlock(p); err != nil {
			return err
		}
	}
	below := fluidAdjacent(p, 0)
	floor, err := r.BlockAt(below)
	if err != nil {
		return err
	}
	infinite := floor.Name == "minecraft:netherrack" || floor.Name == "minecraft:magma_block" || (floor.Name == "minecraft:bedrock" && r.world.DimensionName() == "minecraft:the_end")
	age64, _ := strconv.ParseInt(s.Properties["age"], 10, 32)
	age := int32(age64)
	rng := r.world.tickRandom
	if !infinite {
		wet, err := r.nearRain(p)
		if err != nil {
			return err
		}
		if wet && rng.NextFloat() < .2+float32(age)*.03 {
			return r.removeFireBlock(p)
		}
	}
	if next := min(int32(15), age+rng.NextIntBound(3)/2); next != age {
		s.Properties["age"] = strconv.Itoa(int(next))
		if err := r.SetFluidBlock(p, s, 4); err != nil {
			return err
		}
	}
	if !infinite {
		odds, err := r.maxIgniteOdds(p)
		if err != nil {
			return err
		}
		if odds == 0 {
			if age > 3 || !valid {
				return r.removeFireBlock(p)
			}
			return nil
		}
		if age == 15 && rng.NextIntBound(4) == 0 {
			fuel, err := r.fuelAt(below)
			if err != nil {
				return err
			}
			if fuel[0] == 0 {
				return r.removeFireBlock(p)
			}
		}
	}
	biome, err := r.world.biomeAt(p)
	if err != nil {
		return err
	}
	switch biome {
	case "minecraft:bamboo_jungle", "minecraft:mushroom_fields", "minecraft:mangrove_swamp", "minecraft:snowy_slopes", "minecraft:frozen_peaks", "minecraft:jagged_peaks", "minecraft:swamp", "minecraft:jungle":
		return r.spreadFire(p, age, true)
	default:
		return r.spreadFire(p, age, false)
	}
}
