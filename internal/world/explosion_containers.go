package world

import (
	"atlas/internal/item"
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"atlas/internal/worldgen/randomsource"
	"maps"
)

func (r *fluidRuntime) blastBlockEntity(p worldgen.FeaturePosition) (nbt.Object, error) {
	var data nbt.Object
	err := r.world.fallingEntityOperation(p.X>>4, p.Z>>4, func(_ *Region, ch *Chunk) error {
		root, _ := ch.anvilRoot.Object()
		list, _ := root["block_entities"].Elements()
		for _, t := range list {
			be, _ := t.Object()
			x, _ := be.Number("x")
			y, _ := be.Number("y")
			z, _ := be.Number("z")
			if x == int64(p.X) && y == int64(p.Y) && z == int64(p.Z) {
				data = be
				break
			}
		}
		return nil
	})
	return data, err
}

func blastComponents(functions []blastFunction, be nbt.Object, state worldgen.SurfaceBlockState) nbt.Object {
	var components nbt.Object
	put := func(key string, tag nbt.Tag) {
		if tag.Type == nbt.End {
			return
		}
		if components == nil {
			components = nbt.Object{}
		}
		components[key] = tag
	}
	stored, _ := be.Object("components")
	for _, f := range functions {
		if f.Function == "minecraft:copy_state" {
			props := nbt.Object{}
			for _, key := range f.Properties {
				if value, ok := state.Properties[key]; ok {
					props[key] = nbt.Tag{Type: nbt.String, Value: value}
				}
			}
			if len(props) > 0 {
				put("minecraft:block_state", nbt.Tag{Type: nbt.Compound, Value: props})
			}
		}
		if f.Function != "minecraft:copy_components" {
			continue
		}
		for _, key := range f.Include {
			put(key, stored[key])
			switch key {
			case "minecraft:custom_name":
				put(key, be["CustomName"])
			case "minecraft:banner_patterns":
				put(key, be["patterns"])
			case "minecraft:bees":
				put(key, be["bees"])
			case "minecraft:pot_decorations":
				put(key, be["sherds"])
			case "minecraft:profile":
				put(key, be["profile"])
			case "minecraft:note_block_sound":
				put(key, be["note_block_sound"])
			case "minecraft:lock":
				put(key, be["Lock"])
			case "minecraft:container_loot":
				if table, ok := be.Text("LootTable"); ok {
					loot := nbt.Object{"loot_table": {Type: nbt.String, Value: table}}
					if seed, ok := be.Number("LootTableSeed"); ok && seed != 0 {
						loot["seed"] = be["LootTableSeed"]
					}
					put(key, nbt.Tag{Type: nbt.Compound, Value: loot})
				}
			case "minecraft:container":
				var contents []nbt.Tag
				items, _ := be["Items"].Elements()
				for _, t := range items {
					stack, _ := t.Object()
					slot, _ := stack.Number("Slot")
					stack = maps.Clone(stack)
					delete(stack, "Slot")
					contents = append(contents, nbt.Tag{Type: nbt.Compound, Value: nbt.Object{
						"slot": {Type: nbt.Int, Value: int32(slot)}, "item": {Type: nbt.Compound, Value: stack},
					}})
				}
				if len(contents) > 0 {
					put(key, nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: contents})
				}
			}
		}
	}
	return components
}

// Containers spill their inventory during onRemove, before the explosion
// emits its merged loot. Shulker boxes instead copy their inventory onto the item.
func (r *fluidRuntime) spillBlastContainer(p worldgen.FeaturePosition, name string, be nbt.Object) ([]WorldEntity, error) {
	var size int
	switch name {
	case "minecraft:chest", "minecraft:trapped_chest", "minecraft:barrel":
		size = 27
	case "minecraft:dispenser", "minecraft:dropper", "minecraft:crafter":
		size = 9
	case "minecraft:hopper", "minecraft:brewing_stand":
		size = 5
	case "minecraft:furnace", "minecraft:blast_furnace", "minecraft:smoker":
		size = 3
	default:
		return nil, nil
	}
	slots := make([]nbt.Object, size)
	if table, ok := be.Text("LootTable"); ok {
		seed, _ := be.Number("LootTableSeed")
		inventory := make([]item.Stack, size)
		var err error
		if seed != 0 {
			err = fillContainerLoot(inventory, table, 0, worldgen.NewLegacyRandom(seed))
		} else {
			err = r.world.randomSequences.withSequence(table, func(rng *randomsource.Xoroshiro) error { return fillContainerLoot(inventory, table, 0, rng) })
		}
		if err != nil {
			return nil, err
		}
		for i, stack := range inventory {
			if stack.Empty() {
				continue
			}
			t, err := stack.ToNBT()
			if err != nil {
				return nil, err
			}
			slots[i], _ = t.Object()
		}
	} else {
		items, _ := be["Items"].Elements()
		for _, t := range items {
			stack, _ := t.Object()
			slot, ok := stack.Number("Slot")
			if !ok || slot < 0 || slot >= int64(size) {
				continue
			}
			slots[slot] = stack
		}
	}
	var result []WorldEntity
	for _, slot := range slots {
		items, err := worldgen.DropContainerSlot(p, slot, r.world.tickRandom)
		if err != nil {
			return nil, err
		}
		for _, data := range items {
			e, err := worldEntityFromNBT(data)
			if err != nil {
				return nil, err
			}
			result = append(result, e)
		}
	}
	return result, nil
}
