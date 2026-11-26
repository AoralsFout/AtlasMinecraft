package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"slices"
)

// Vanilla's on_mob_spawn_equipment tag in encounter order, limited to
// enchantments whose primary item is a bow or a piece of skeleton armor.
type trapEnchantment struct {
	name                                            string
	slots, weight, maxLevel, minCost, maxCost, step int32
	protection                                      bool
}

var trapEnchantments = []trapEnchantment{
	{"protection", 15, 10, 4, 1, 12, 11, true}, {"fire_protection", 15, 5, 4, 10, 18, 8, true},
	{"feather_falling", 1, 5, 4, 5, 11, 6, false}, {"blast_protection", 15, 2, 4, 5, 13, 8, true},
	{"projectile_protection", 15, 5, 4, 3, 9, 6, true}, {"respiration", 8, 2, 3, 10, 40, 10, false},
	{"aqua_affinity", 8, 2, 1, 1, 41, 0, false}, {"thorns", 4, 1, 3, 10, 60, 20, false},
	{"depth_strider", 1, 2, 3, 10, 25, 10, false}, {"unbreaking", 31, 5, 3, 5, 55, 8, false},
	{"power", 16, 10, 5, 1, 16, 10, false}, {"punch", 16, 2, 2, 12, 37, 20, false},
	{"flame", 16, 2, 1, 20, 50, 0, false}, {"infinity", 16, 1, 1, 20, 50, 0, false},
}

func equipTrapRider(data nbt.Object, difficulty float32, hard bool, rng *worldgen.LegacyRandom) {
	special := min(max((difficulty-2)/2, 0), 1)
	material := 3 // fallback iron helmet
	var pieces [4]bool
	if rng.NextFloat() < 0.15*special {
		material = int(rng.NextIntBound(2))
		for range 3 {
			if rng.NextFloat() < 0.095 {
				material++
			}
		}
		stop := float32(.25)
		if hard {
			stop = .1
		}
		for slot := range pieces {
			if slot > 0 && rng.NextFloat() < stop {
				break
			}
			pieces[slot] = true
		}
	}
	armor := make([]nbt.Tag, 4)
	for i, piece := range []string{"boots", "leggings", "chestplate", "helmet"} {
		if !pieces[i] && i < 3 {
			armor[i] = nbt.Tag{Type: nbt.Compound, Value: nbt.Object{}}
			continue
		}
		tier := material
		if !pieces[i] {
			tier = 3
		}
		name := []string{"leather", "golden", "chainmail", "iron", "diamond"}[tier] + "_" + piece
		stack := nbt.Object{"id": {Type: nbt.String, Value: "minecraft:" + name}, "count": {Type: nbt.Int, Value: int32(1)}}
		if i == 3 || rng.NextFloat() < .5*special {
			enchantTrapItem(stack, 1<<i, []int32{15, 25, 12, 9, 10}[tier], special, rng)
		}
		armor[i] = nbt.Tag{Type: nbt.Compound, Value: stack}
	}
	bow := nbt.Object{"id": {Type: nbt.String, Value: "minecraft:bow"}, "count": {Type: nbt.Int, Value: int32(1)}}
	enchantTrapItem(bow, 16, 1, special, rng)
	data["HandItems"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{{Type: nbt.Compound, Value: bow}, {Type: nbt.Compound, Value: nbt.Object{}}}}
	data["ArmorItems"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: armor}
}

func enchantTrapItem(stack nbt.Object, slot, enchantability int32, special float32, rng *worldgen.LegacyRandom) {
	cost := int32(5)
	if span := int32(special * 17); span > 0 {
		cost += rng.NextIntBound(span + 1)
	}
	cost += 1 + rng.NextIntBound(enchantability/4+1) + rng.NextIntBound(enchantability/4+1)
	cost = max(1, int32(float32(cost)+float32(cost)*((rng.NextFloat()+rng.NextFloat()-1)*.15)+.5))
	type candidate struct {
		trapEnchantment
		level int32
	}
	var pool []candidate
	for _, e := range trapEnchantments {
		if e.slots&slot == 0 {
			continue
		}
		for level := e.maxLevel; level > 0; level-- {
			offset := (level - 1) * e.step
			if cost >= e.minCost+offset && cost <= e.maxCost+offset {
				pool = append(pool, candidate{e, level})
				break
			}
		}
	}
	levels := nbt.Object{}
	for len(pool) > 0 {
		var total int32
		for _, e := range pool {
			total += e.weight
		}
		pick := rng.NextIntBound(total)
		var selected candidate
		for _, e := range pool {
			pick -= e.weight
			if pick < 0 {
				selected = e
				break
			}
		}
		levels["minecraft:"+selected.name] = nbt.Tag{Type: nbt.Int, Value: selected.level}
		if len(levels) > 1 {
			cost /= 2
		}
		if rng.NextIntBound(50) > cost {
			break
		}
		pool = slices.DeleteFunc(pool, func(e candidate) bool { return e.name == selected.name || e.protection && selected.protection })
	}
	if len(levels) > 0 {
		stack["components"] = nbt.Tag{Type: nbt.Compound, Value: nbt.Object{"minecraft:enchantments": {Type: nbt.Compound, Value: nbt.Object{"levels": {Type: nbt.Compound, Value: levels}}}}}
	}
}
