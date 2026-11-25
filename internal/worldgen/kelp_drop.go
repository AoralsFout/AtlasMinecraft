package worldgen

import (
	"atlas/internal/nbt"
	"fmt"
)

// NewKelpBlockDropEntity follows Block.popResource for a non-explosion kelp
// break. Both original block loot tables yield exactly one kelp. Position draws
// occur even when doTileDrops=false; motion/UUID draws require an actual item.
func NewKelpBlockDropEntity(p FeaturePosition, drops bool, worldRandom, entityRandom *LegacyRandom) (nbt.Object, error) {
	return NewBlockDropEntity(p, "minecraft:kelp", 1, drops, worldRandom, entityRandom)
}

// NewBlockDropEntity follows Block.popResource for a stack yielded by a block loot table.
func NewBlockDropEntity(p FeaturePosition, item string, count int32, drops bool, worldRandom, entityRandom *LegacyRandom) (nbt.Object, error) {
	if worldRandom == nil {
		return nil, fmt.Errorf("block drop requires world randomness")
	}
	position := [3]float64{
		float64(p.X) + 0.5 + (-0.25 + worldRandom.NextDouble()*0.5),
		float64(p.Y) + 0.5 + (-0.25 + worldRandom.NextDouble()*0.5) - 0.125,
		float64(p.Z) + 0.5 + (-0.25 + worldRandom.NextDouble()*0.5),
	}
	if !drops {
		return nil, nil
	}
	entity, err := newDroppedItemEntity(position, SurfaceBlockState{Name: item}, worldRandom, entityRandom)
	if err != nil {
		return nil, err
	}
	stack, _ := entity.Object("Item")
	stack["count"] = nbt.Tag{Type: nbt.Int, Value: count}
	return entity, nil
}
