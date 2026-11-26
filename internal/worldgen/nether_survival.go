package worldgen

import (
	"atlas/internal/blockrules"
	"atlas/internal/blockstate"
	_ "embed"
	"encoding/base64"
	"strings"
)

//go:embed fire_burnable_1.21.1.txt
var fireBurnableData string

// FireBlock.canBurn differs per state: waterlogged stairs/leaves/slabs do not
// burn, and Nether wood is not combustible. Extracted from the actual JAR.
var fireBurnableStates = func() []byte {
	f := strings.Fields(fireBurnableData)
	if len(f) != 3 || f[0] != "fire_burnable_states" || f[1] != "26684" {
		panic("invalid fire burnable states")
	}
	bits, err := base64.StdEncoding.DecodeString(f[2])
	if err != nil || len(bits) != (26684+7)/8 {
		panic("invalid fire burnable state bits")
	}
	return bits
}()

func fireCanBurn(state SurfaceBlockState) (bool, error) {
	id, err := blockstate.BlockStateID(state.Name, state.Properties)
	if err != nil {
		return false, err
	}
	return fireBurnableStates[id/8]&(1<<uint(id%8)) != 0, nil
}

// FirePlacement returns BaseFireBlock.getState and whether it can survive.
// With neither a solid nor a combustible floor, fire attaches to each
// combustible side and ceiling. Waterlogging affects the individual state.
func FirePlacement(world FeatureBlockWorld, p FeaturePosition) (SurfaceBlockState, bool, error) {
	fire := SurfaceBlockState{Name: "minecraft:fire"}
	below, err := world.BlockAt(directionPosition(p, 0))
	if err != nil {
		return fire, false, err
	}
	if below.Name == "minecraft:soul_sand" || below.Name == "minecraft:soul_soil" {
		return SurfaceBlockState{Name: "minecraft:soul_fire"}, true, nil
	}
	id, err := blockstate.BlockStateID(below.Name, below.Properties)
	if err != nil {
		return fire, false, err
	}
	rule, err := blockrules.Lookup(id)
	if err != nil {
		return fire, false, err
	}
	if rule.FaceSturdy[1] || fireBurnableStates[id/8]&(1<<uint(id%8)) != 0 {
		return fire, true, nil
	}
	for face, property := range []string{"", "up", "north", "south", "west", "east"} {
		if face == 0 {
			continue
		}
		state, err := world.BlockAt(directionPosition(p, face))
		if err != nil {
			return fire, false, err
		}
		attaches, err := fireCanBurn(state)
		if err != nil {
			return fire, false, err
		}
		if attaches {
			if fire.Properties == nil {
				fire.Properties = make(map[string]string)
			}
			fire.Properties[property] = "true"
		}
	}
	return fire, len(fire.Properties) > 0, nil
}

func featureFireSurvives(world FeatureBlockWorld, p FeaturePosition, below SurfaceBlockState) (bool, error) {
	id, err := blockstate.BlockStateID(below.Name, below.Properties)
	if err != nil {
		return false, err
	}
	rule, err := blockrules.Lookup(id)
	if err != nil {
		return false, err
	}
	if rule.FaceSturdy[1] {
		return true, nil
	}
	for d := range featureDirections {
		s, err := world.BlockAt(directionPosition(p, d))
		if err != nil {
			return false, err
		}
		burnable, err := fireCanBurn(s)
		if err != nil {
			return false, err
		}
		if burnable {
			return true, nil
		}
	}
	return false, nil
}

func featureNetherPlantSurvives(name string, below SurfaceBlockState) bool {
	ok := netherNylium(below) || below.Name == "minecraft:soul_soil" || PlantGroundSurvives(name, below.Name)
	if name == "minecraft:crimson_fungus" || name == "minecraft:warped_fungus" {
		ok = ok || below.Name == "minecraft:mycelium"
	}
	return ok
}
