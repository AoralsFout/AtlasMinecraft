package worldgen

import (
	"atlas/internal/nbt"
	"maps"
	"math/rand/v2"
)

// DropContainerSlot follows Containers.dropItemStack. Empty slots also draw
// their three offsets. All pieces of a single slot start at the same position.
func DropContainerSlot(p FeaturePosition, stack nbt.Object, worldRandom *LegacyRandom) ([]nbt.Object, error) {
	pos := [3]float64{float64(p.X) + worldRandom.NextDouble()*.75 + .125,
		float64(p.Y) + worldRandom.NextDouble()*.75, float64(p.Z) + worldRandom.NextDouble()*.75 + .125}
	count, _ := stack.Number("count")
	name, _ := stack.Text("id")
	var result []nbt.Object
	for count > 0 {
		n := min(count, int64(worldRandom.NextIntBound(21)+10))
		count -= n
		entity, err := newDroppedItemEntity(pos, SurfaceBlockState{Name: name}, worldRandom, NewLegacyRandom(rand.Int64()))
		if err != nil {
			return nil, err
		}
		split := maps.Clone(stack)
		delete(split, "Slot")
		split["count"] = nbt.Tag{Type: nbt.Int, Value: int32(n)}
		entity["Item"] = nbt.Tag{Type: nbt.Compound, Value: split}
		motion, _ := entity["Motion"].Elements()
		for i, mean := range [3]float64{0, .2, 0} {
			motion[i].Value = mean + .11485000171139836*(worldRandom.NextDouble()-worldRandom.NextDouble())
		}
		entity["PickupDelay"] = nbt.Tag{Type: nbt.Short, Value: int16(0)}
		result = append(result, entity)
	}
	return result, nil
}
