package world

import (
	"atlas/internal/nbt"
	_ "embed"
	"encoding/json"
	"maps"
	"math"
)

//go:embed living_damage_1.21.1.json
var livingDamageJSON []byte

type livingTrait struct {
	Armor      float64
	FireImmune bool
}

type armorPiece struct {
	Slot             string
	Armor, Toughness float64
}
type damageDefaults struct {
	Mobs      map[string]livingTrait
	Equipment map[string]armorPiece
}

var livingDefaults = func() damageDefaults {
	var m damageDefaults
	if err := json.Unmarshal(livingDamageJSON, &m); err != nil {
		panic(err)
	}
	return m
}()

type damageModifiers struct{ add, base, total float64 }

func (m *damageModifiers) apply(operation string, value float64) {
	switch operation {
	case "add_value":
		m.add += value
	case "add_multiplied_base":
		m.base += value
	case "add_multiplied_total":
		m.total *= 1 + value
	}
}

func eachLivingEquipment(d nbt.Object, visit func(nbt.Object, string)) {
	for _, group := range []struct {
		key   string
		slots []string
	}{
		{"ArmorItems", []string{"feet", "legs", "chest", "head"}},
		{"HandItems", []string{"mainhand", "offhand"}},
	} {
		items, _ := d[group.key].Elements()
		for i, tag := range items {
			if i >= len(group.slots) {
				break
			}
			item, _ := tag.Object()
			if _, ok := item.Text("id"); ok {
				visit(item, group.slots[i])
			}
		}
	}
	if body, ok := d.Object("body_armor_item"); ok {
		visit(body, "body")
	}
}

func armorSlot(slot string) bool {
	return slot == "feet" || slot == "legs" || slot == "chest" || slot == "head"
}

func enchantmentLevel(components nbt.Object, name string) int64 {
	enchantments, _ := components.Object("minecraft:enchantments")
	if levels, ok := enchantments.Object("levels"); ok {
		enchantments = levels
	}
	n, _ := enchantments.Number(name)
	return max(0, min(n, 255))
}

func livingAttribute(e WorldEntity, name string, base float64) float64 {
	mods := damageModifiers{total: 1}
	attrs, _ := e.Data["attributes"].Elements()
	for _, tag := range attrs {
		a, _ := tag.Object()
		if id, _ := a.Text("id"); id == name {
			if value, ok := a["base"].Value.(float64); ok {
				base = value
			}
			list, _ := a["modifiers"].Elements()
			for _, entry := range list {
				m, _ := entry.Object()
				op, _ := m.Text("operation")
				value, _ := m["amount"].Value.(float64)
				mods.apply(op, value)
			}
			break
		}
	}
	eachLivingEquipment(e.Data, func(stack nbt.Object, slot string) {
		id, _ := stack.Text("id")
		components, _ := stack.Object("components")
		if custom, ok := components["minecraft:attribute_modifiers"]; ok {
			if d, ok := custom.Object(); ok {
				custom = d["modifiers"]
			}
			values, _ := custom.Elements()
			for _, tag := range values {
				m, _ := tag.Object()
				attr, _ := m.Text("type")
				if attr != name {
					continue
				}
				group, _ := m.Text("slot")
				if group != "" && group != "any" && group != slot && !(group == "armor" && armorSlot(slot)) && !(group == "hand" && (slot == "offhand" || slot == "mainhand")) {
					continue
				}
				op, _ := m.Text("operation")
				value, _ := m["amount"].Value.(float64)
				mods.apply(op, value)
			}
		} else if _, removed := components["!minecraft:attribute_modifiers"]; !removed {
			if piece := livingDefaults.Equipment[id]; piece.Slot == slot {
				switch name {
				case "minecraft:generic.armor":
					mods.add += piece.Armor
				case "minecraft:generic.armor_toughness":
					mods.add += piece.Toughness
				}
			}
		}
		if name == "minecraft:generic.burning_time" && armorSlot(slot) {
			mods.base -= .15 * float64(enchantmentLevel(components, "minecraft:fire_protection"))
		}
	})
	return (base + mods.add) * (1 + mods.base) * mods.total
}

func thunderHit(e *WorldEntity, t livingTrait, cooldown *hurtCooldown, bolt *lightningImpact) (bool, string) {
	// These entities dispatch thunderHit to their own conversion/decoration logic.
	switch e.Type {
	case "minecraft:armor_stand", "minecraft:mooshroom":
		return false, ""
	case "minecraft:pig", "minecraft:villager":
		if !bolt.peaceful {
			return false, ""
		}
	}
	e.Data = maps.Clone(e.Data)
	fire, _ := e.Data.Number("Fire")
	fire++
	if fire == 0 {
		fire = int64(math.Floor(160 * max(0, min(1024, livingAttribute(*e, "minecraft:generic.burning_time", 1)))))
	}
	amount := float32(5)
	if e.Type == "minecraft:turtle" {
		amount = math.MaxFloat32
	} // Turtle.thunderHit calls hurt directly.
	if e.Type != "minecraft:turtle" {
		e.Data["Fire"] = nbt.Tag{Type: nbt.Short, Value: int16(fire)}
	}
	if e.Type == "minecraft:creeper" {
		e.Data["powered"] = byteTag(1)
	}
	return true, applyLivingDamage(e, t, cooldown, amount, "lightning_bolt")
}

func advanceLivingDamage(e *WorldEntity, t livingTrait, c *hurtCooldown, wet bool) (changed, removed bool, source string) {
	fire, _ := e.Data.Number("Fire")
	hurt, _ := e.Data.Number("HurtTime")
	hp, _ := e.Data["Health"].Value.(float32)
	c.burning = fire > 1 && !t.FireImmune
	if fire > 0 || hurt > 0 || hp <= 0 || wet && fire != 0 {
		e.Data = maps.Clone(e.Data)
		changed = true
		if fire > 0 {
			if !t.FireImmune && fire%20 == 0 && hp > 0 {
				source = applyLivingDamage(e, t, c, 1, "on_fire")
			}
			fire--
			e.Data["Fire"] = nbt.Tag{Type: nbt.Short, Value: int16(fire)}
		}
		if wet || t.FireImmune {
			e.Data["Fire"] = nbt.Tag{Type: nbt.Short, Value: int16(0)}
		}
		if hurt, _ = e.Data.Number("HurtTime"); hurt > 0 {
			e.Data["HurtTime"] = nbt.Tag{Type: nbt.Short, Value: int16(hurt - 1)}
		}
		if hp, _ = e.Data["Health"].Value.(float32); hp <= 0 {
			death, _ := e.Data.Number("DeathTime")
			death++
			e.Data["DeathTime"] = nbt.Tag{Type: nbt.Short, Value: int16(death)}
			removed = death >= 20
		}
	}
	if c.ticks > 0 {
		c.ticks--
	}
	return
}

func applyLivingDamage(e *WorldEntity, t livingTrait, c *hurtCooldown, amount float32, source string) string {
	if inv, _ := e.Data.Number("Invulnerable"); inv != 0 {
		return ""
	}
	effects, _ := e.Data["active_effects"].Elements()
	resistance := int64(0)
	for _, tag := range effects {
		d, _ := tag.Object()
		id, _ := d.Text("id")
		if id == "minecraft:fire_resistance" && source == "on_fire" {
			return ""
		}
		if id == "minecraft:resistance" {
			n, _ := d.Number("amplifier")
			resistance = n + 1
		}
	}
	notify := source
	if c.ticks > 10 {
		if amount <= c.amount {
			return ""
		}
		previous := c.amount
		c.amount = amount
		amount -= previous
		notify = ""
	} else {
		c.ticks, c.amount = 20, amount
		e.Data["HurtTime"] = nbt.Tag{Type: nbt.Short, Value: int16(10)}
	}
	if source == "lightning_bolt" {
		armor := float32(math.Floor(max(0, min(30, livingAttribute(*e, "minecraft:generic.armor", t.Armor)))))
		toughness := float32(max(0, min(20, livingAttribute(*e, "minecraft:generic.armor_toughness", 0))))
		amount *= 1 - min(float32(20), max(armor/5, armor-amount/(2+toughness/4)))/25
	}
	amount = amount * float32(max(0, 25-5*resistance)) / 25
	var protection int64
	eachLivingEquipment(e.Data, func(stack nbt.Object, slot string) {
		if !armorSlot(slot) {
			return
		}
		components, _ := stack.Object("components")
		protection += enchantmentLevel(components, "minecraft:protection")
		if source == "on_fire" {
			protection += 2 * enchantmentLevel(components, "minecraft:fire_protection")
		}
	})
	amount *= 1 - float32(min(20, protection))/25
	absorption, _ := e.Data["AbsorptionAmount"].Value.(float32)
	if absorption > 0 {
		used := min(amount, absorption)
		e.Data["AbsorptionAmount"] = nbt.Tag{Type: nbt.Float, Value: absorption - used}
		amount -= used
	}
	hp, _ := e.Data["Health"].Value.(float32)
	e.Data["Health"] = nbt.Tag{Type: nbt.Float, Value: max(float32(0), hp-amount)}
	return notify
}
