package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	_ "embed"
	"fmt"
	"maps"
)

//go:embed zombified_piglin_1.21.1.bin
var zombifiedPiglinDefaults []byte

func lightningReplacement(old WorldEntity, bolt *lightningImpact) (WorldEntity, error) {
	var data nbt.Object
	kind := "minecraft:zombified_piglin"
	if old.Type == "minecraft:pig" {
		tag, _, err := nbt.ParseNetwork(zombifiedPiglinDefaults)
		if err != nil {
			return WorldEntity{}, err
		}
		data, _ = tag.Object()
		sword := nbt.Object{"id": {Type: nbt.String, Value: "minecraft:golden_sword"}, "count": {Type: nbt.Int, Value: int32(1)}}
		data["HandItems"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{{Type: nbt.Compound, Value: sword}, {Type: nbt.Compound, Value: nbt.Object{}}}}
		if age, _ := old.Data.Number("Age"); age < 0 {
			data["IsBaby"] = byteTag(1)
		}
		// Zombie.setBaby accesses movement speed even for an adult. Its
		// baby modifier is transient, but the base is saved.
		speed := nbt.Object{"id": {Type: nbt.String, Value: "minecraft:generic.movement_speed"}, "base": {Type: nbt.Double, Value: float64(float32(.23))}}
		data["attributes"] = mergeEntityAttributes(data["attributes"], nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{{Type: nbt.Compound, Value: speed}}})
	} else {
		kind = "minecraft:witch"
		var err error
		data, _, err = defaultGeneratedEntity(kind)
		if err != nil {
			return WorldEntity{}, err
		}
		if data == nil {
			return WorldEntity{}, fmt.Errorf("missing witch constructor defaults")
		}
		// PatrollingMonster.finalizeSpawn rolls the leader chance before
		// checking canBeLeader, even for a witch spawned by lightning.
		bolt.random.NextFloat()
		maps.Copy(data, worldgen.MobSpawnAttributes(bolt.random))
		data["CanJoinRaid"] = byteTag(1)
	}
	id, _, err := newLightningUUID()
	if err != nil {
		return WorldEntity{}, err
	}
	data["id"] = nbt.Tag{Type: nbt.String, Value: kind}
	data["UUID"] = entityUUIDTag(id)
	data["Pos"] = entityPositionTag(old.Position)
	data["Rotation"] = old.Data["Rotation"]
	data["PersistenceRequired"] = byteTag(1)
	if noAI, _ := old.Data.Number("NoAI"); noAI != 0 {
		data["NoAI"] = byteTag(1)
	}
	if name, ok := old.Data.Text("CustomName"); ok && name != "" {
		data["CustomName"] = old.Data["CustomName"]
		if visible, _ := old.Data.Number("CustomNameVisible"); visible != 0 {
			data["CustomNameVisible"] = byteTag(1)
		}
	}
	e, err := worldEntityFromNBT(data)
	e.RuntimeID = NextRuntimeEntityID()
	e.afterBolt = bolt.lastOrder
	return e, err
}

func (ch *Chunk) convertMooshroom(e *WorldEntity, bolt *lightningImpact, changes *[]LivingDamageChange) bool {
	if previous, ok := ch.lastMooshroomBolt[e.UUID]; ok && previous == bolt.uuid {
		return false
	}
	if ch.lastMooshroomBolt == nil {
		ch.lastMooshroomBolt = make(map[[16]byte][16]byte)
	}
	ch.lastMooshroomBolt[e.UUID] = bolt.uuid
	variant := "brown"
	if old, _ := e.Data.Text("Type"); old == "brown" {
		variant = "red"
	}
	e.Data = maps.Clone(e.Data)
	e.Data["Type"] = nbt.Tag{Type: nbt.String, Value: variant}
	change := LivingDamageChange{Entity: *e, MooshroomVariant: variant}
	change.Entity.Revision++
	if silent, _ := e.Data.Number("Silent"); silent == 0 {
		change.SoundSeed = bolt.nextSoundSeed()
	}
	*changes = append(*changes, change)
	return true
}
