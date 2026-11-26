package worldgen

import "strconv"

// UpdateFluidNeighborShape applies immediate plant callbacks, schedules leaf
// and falling-block updates, and reuses the existing structure block shapes.
// Redstone and other delayed families still need their scheduled handlers.
func UpdateFluidNeighborShape(world FeatureBlockWorld, p FeaturePosition, state SurfaceBlockState, face int, neighbor SurfaceBlockState) (SurfaceBlockState, error) {
	if state.Name == "minecraft:soul_fire" {
		below := neighbor
		if face != 0 {
			var err error
			below, err = world.BlockAt(directionPosition(p, 0))
			if err != nil {
				return state, err
			}
		}
		if below.Name != "minecraft:soul_soil" && below.Name != "minecraft:soul_sand" {
			return structureState("air"), nil
		}
		return state, nil
	}
	if state.Name == "minecraft:fire" {
		fire, supported, err := FirePlacement(world, p)
		if err != nil {
			return state, err
		}
		if !supported {
			return structureState("air"), nil
		}
		if age := state.Properties["age"]; fire.Name == state.Name && age != "" {
			fire = stateWithProperty(fire, "age", age)
		}
		return fire, nil
	}
	direction := [6]string{"down", "up", "north", "south", "west", "east"}[face]
	if direction == "up" {
		if _, ok := state.Properties["snowy"]; ok {
			snowy := neighbor.Name == "minecraft:snow" || neighbor.Name == "minecraft:snow_block" || neighbor.Name == "minecraft:powder_snow"
			return stateWithProperty(state, "snowy", strconv.FormatBool(snowy)), nil
		}
	}
	if direction == "down" && state.Name == "minecraft:snow" {
		supported, err := SnowSupported(neighbor)
		if err != nil {
			return state, err
		}
		if !supported {
			return structureState("air"), nil
		}
	}
	if state.Name == "minecraft:farmland" && direction == "up" {
		return state, updateFarmlandCover(world, p, neighbor)
	}
	if kelpBlock(state.Name) {
		return updateKelpNeighborShape(world, p, state, direction, neighbor)
	}
	if brushableBlock(state.Name) {
		return state, NotifyBrushableBlockPlacement(world, p, state)
	}
	if fallingBlock(state.Name) {
		return updateFallingNeighborShape(world, p, state)
	}
	if state.Name == "minecraft:magma_block" || state.Name == "minecraft:soul_sand" || state.Name == "minecraft:bubble_column" {
		return updateBubbleNeighborShape(world, p, state, face, neighbor)
	}
	if IsLeafBlock(state.Name) {
		return state, UpdateLeafNeighbor(world, p, state, neighbor)
	}
	if runtimePlantShape(state.Name) {
		return updateRuntimePlantShape(world, p, state, direction, neighbor)
	}
	return mansionTemplateUpdateShape(world, p, state, direction, neighbor)
}
