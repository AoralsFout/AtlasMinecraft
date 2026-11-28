package item

import (
	"atlas/internal/nbt"
	"atlas/internal/proto"
	"math/rand/v2"
	"slices"
)

const FlintAndSteel = "minecraft:flint_and_steel"

// UseFlintAndSteel applies one use to a non-creative player's tool.
func (s Stack) UseFlintAndSteel() (Stack, error) {
	name, _ := Name(s.ID)
	if s.Empty() || name != FlintAndSteel {
		return s, nil
	}
	var damageStart, damageEnd int
	values, removed, err := readComponents(proto.NewReader(s.Patch), 0, func(name string, start, end int) {
		if name == "damage" {
			damageStart, damageEnd = start, end
		}
	})
	if err != nil {
		return s, err
	}
	if _, unbreakable := values["unbreakable"]; unbreakable || slices.Contains(removed, "max_damage") || slices.Contains(removed, "damage") {
		return s, nil
	}
	maxDamage, ok := values["max_damage"].(int32)
	if !ok {
		maxDamage = 64
	}
	if enchantments, ok := values["enchantments"].(map[string]any); ok {
		for _, value := range enchantments["enchantments"].([]any) {
			entry := value.(map[string]any)
			name, _ := registryName("enchantment", entry["id"].(int32))
			level := entry["level"].(int32)
			if name == "minecraft:unbreaking" && level > 0 && rand.Int64N(int64(level)+1) != 0 {
				return s, nil
			}
		}
	}
	damage, _ := values["damage"].(int32)
	if max(damage, 0) >= maxDamage-1 {
		return s.WithCount(s.Count - 1), nil
	}
	damage = max(damage, 0) + 1
	patch := proto.NewWriter()
	if damageEnd != 0 {
		patch.Raw(s.Patch[:damageStart]).VarInt(damage).Raw(s.Patch[damageEnd:])
	} else {
		r := proto.NewReader(s.Patch)
		_, _ = r.VarInt()
		_, _ = r.VarInt()
		damageID, _ := componentID("damage")
		patch.VarInt(int32(len(values) + 1)).VarInt(int32(len(removed))).VarInt(damageID).VarInt(damage).Raw(r.Rest())
	}
	if len(s.Saved) != 0 {
		_, tag, err := nbt.Parse(s.Saved)
		if err != nil {
			return s, err
		}
		o, _ := tag.Object()
		components, ok := o.Object("components")
		if !ok {
			components = nbt.Object{}
			o["components"] = compound(components)
		}
		delete(components, "damage")
		components["minecraft:damage"] = integer(damage)
		saved, err := nbt.Marshal("", tag)
		if err != nil {
			return s, err
		}
		s.Saved = saved
	}
	s.Patch = patch.Payload()
	return s, nil
}
