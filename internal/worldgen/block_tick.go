package worldgen

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

//go:embed block_tick_handlers_1.21.1.json
var blockTickHandlerData []byte

// Original method owners distinguish inherited no-op handlers from handlers
// not ported yet. Unknown behavior must not silently consume a saved request.
var blockTickHandlers = func() map[string]string {
	var groups map[string][]string
	if err := json.Unmarshal(blockTickHandlerData, &groups); err != nil {
		panic(err)
	}
	result := make(map[string]string)
	for owner, names := range groups {
		for _, name := range names {
			if result[name] != "" {
				panic("duplicate block tick handler")
			}
			result[name] = owner
		}
	}
	return result
}()

var ErrUnsupportedScheduledBlock = errors.New("scheduled block handler is not implemented")

type ScheduledBlockWorld interface {
	BlockAt(FeaturePosition) (SurfaceBlockState, error)
	SetFluidBlock(FeaturePosition, SurfaceBlockState, int32) error
}

// TickScheduledBlock runs BlockState.tick only when the current block matches
// the scheduled block type. Leaf decay is a separate random tick operation.
func TickScheduledBlock(world ScheduledBlockWorld, p FeaturePosition, name string) error {
	state, err := world.BlockAt(p)
	if err != nil {
		return err
	}
	if state.Name != name {
		return nil
	}
	if IsStoneButton(name) {
		if state.Properties["powered"] != "true" {
			return nil
		}
		buttonWorld, ok := world.(interface {
			NotifyButtonNeighbors(FeaturePosition, SurfaceBlockState) error
		})
		if !ok {
			return fmt.Errorf("stone button release requires neighbor notifications")
		}
		if err := world.SetFluidBlock(p, stateWithProperty(state, "powered", "false"), 3); err != nil {
			return err
		}
		return buttonWorld.NotifyButtonNeighbors(p, state)
	}
	switch blockTickHandlers[name] {
	case "diy":
		fire, ok := world.(interface {
			TickFire(FeaturePosition, SurfaceBlockState) error
		})
		if !ok {
			return fmt.Errorf("fire tick requires weather and world randomness")
		}
		return fire.TickFire(p, state)
	case "dkn":
		updates, ok := world.(interface {
			NotifyRodSupport(FeaturePosition, SurfaceBlockState) error
		})
		if !ok {
			return fmt.Errorf("lightning rod release requires support notifications")
		}
		if err := world.SetFluidBlock(p, stateWithProperty(state, "powered", "false"), 3); err != nil {
			return err
		}
		return updates.NotifyRodSupport(p, state)
	case "dtb":
		return nil // BlockBehaviour.tick is empty.
	case "div":
		above, err := world.BlockAt(treeOffset(p, 0, 1, 0))
		if err != nil {
			return err
		}
		covered, err := blocksFarmland(above)
		if err != nil || !covered {
			return err
		}
		return TurnFarmlandToDirt(world, p)
	case "diu":
		return tickFallingBlock(world, p, state)
	case "dgd":
		return tickBrushableBlock(world, p, state)
	case "djl":
		if kelpBlock(name) {
			return tickKelp(world, p, state)
		}
		return fmt.Errorf("%w: %s (original owner djl)", ErrUnsupportedScheduledBlock, name)
	case "dkr", "dnl", "dge":
		w, ok := world.(FeatureBlockWorld)
		if !ok {
			return fmt.Errorf("bubble tick requires bounded block access")
		}
		position, support := treeOffset(p, 0, 1, 0), state
		if state.Name == "minecraft:bubble_column" {
			position = p
			support, err = world.BlockAt(treeOffset(p, 0, -1, 0))
			if err != nil {
				return err
			}
		}
		_, err := UpdateBubbleColumn(w, position, support)
		return err
	case "dki":
		distance := 7
		for _, d := range [6]FeaturePosition{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			neighbor, err := world.BlockAt(treeOffset(p, d.X, d.Y, d.Z))
			if err != nil {
				return err
			}
			distance = min(distance, scheduledLeafDistance(neighbor)+1)
			if distance == 1 {
				break
			}
		}
		return world.SetFluidBlock(p, stateWithProperty(state, "distance", strconv.Itoa(distance)), 3)
	default:
		return fmt.Errorf("%w: %s (original owner %s)", ErrUnsupportedScheduledBlock, name, blockTickHandlers[name])
	}
}

func scheduledLeafDistance(state SurfaceBlockState) int {
	if treeTags["logs"][state.Name] {
		return 0
	}
	if IsLeafBlock(state.Name) {
		if distance, err := strconv.Atoi(state.Properties["distance"]); err == nil {
			return distance
		}
	}
	return 7
}

func IsLeafBlock(name string) bool { return blockTickHandlers[name] == "dki" }

func IsStoneButton(name string) bool {
	return name == "minecraft:stone_button" || name == "minecraft:polished_blackstone_button"
}

func UpdateLeafNeighbor(world FeatureBlockWorld, p FeaturePosition, state, neighbor SurfaceBlockState) error {
	if state.Properties["waterlogged"] == "true" {
		if err := runtimePlantWaterTick(world, p); err != nil {
			return err
		}
	}
	distance := scheduledLeafDistance(neighbor) + 1
	if distance != 1 || scheduledLeafDistance(state) != distance {
		scheduler, ok := world.(interface {
			ScheduleBlockTick(FeaturePosition, string, int32, int32) error
		})
		if !ok {
			return fmt.Errorf("leaf shape requires a block tick scheduler")
		}
		return scheduler.ScheduleBlockTick(p, state.Name, 1, 0)
	}
	return nil
}
