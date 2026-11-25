package worldgen

import (
	"fmt"
	"strings"
)

func blocksFarmland(above SurfaceBlockState) (bool, error) {
	if strings.HasSuffix(above.Name, "_fence_gate") || above.Name == "minecraft:moving_piston" {
		return false, nil
	}
	rules, err := fluidRulesForBlock(above)
	return rules.solid(), err
}

func updateFarmlandCover(world FeatureBlockWorld, p FeaturePosition, above SurfaceBlockState) error {
	covered, err := blocksFarmland(above)
	if err != nil || !covered {
		return err
	}
	scheduler, ok := world.(interface {
		ScheduleBlockTick(FeaturePosition, string, int32, int32) error
	})
	if !ok {
		return fmt.Errorf("farmland cover requires a block tick scheduler")
	}
	return scheduler.ScheduleBlockTick(p, "minecraft:farmland", 1, 0)
}

func TurnFarmlandToDirt(world ScheduledBlockWorld, p FeaturePosition) error {
	if err := world.SetFluidBlock(p, structureState("dirt"), 3); err != nil {
		return err
	}
	if events, ok := world.(interface {
		FallingGameEvent(FeaturePosition, string) error
	}); ok {
		return events.FallingGameEvent(p, "minecraft:block_change")
	}
	return nil
}
