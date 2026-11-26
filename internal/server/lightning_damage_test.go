package server

import (
	"atlas/internal/nbt"
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"bytes"
	"encoding/hex"
	"testing"
)

func TestLightningDamagePacketsForTrackedMob(t *testing.T) {
	s, p := interactionServer(t)
	p.SetPlayReady(true)
	p.MarkChunkSent(0, 0)
	other := player.New("Other", [16]byte{99}, 99, nil)
	other.SetPlayReady(true)
	dimension := s.World.DimensionName()
	cow := world.WorldEntity{Type: "minecraft:cow", UUID: [16]byte{9}, Position: [3]float64{8, 70, 8}, Data: nbt.Object{"Health": {Type: nbt.Float, Value: float32(5)}}}
	key := generatedEntityKey{dimension, cow.UUID}
	view := s.generatedEntityView(p)
	s.spawnGeneratedEntity(p, p.WorldPacketSinkInDimension(dimension), key, cow, view)
	for len(p.Out) > 0 {
		<-p.Out
	}
	id := view[key]
	publish := func(change world.LivingDamageChange) {
		s.publishLivingDamage([]*player.Player{p, other}, dimension, change)
	}
	expect := func(kind int32, want []byte) {
		t.Helper()
		if len(p.Out) == 0 {
			t.Fatal("missing packet", kind)
		}
		got := <-p.Out
		if got.ID != kind || !bytes.Equal(got.Payload, want) {
			t.Fatalf("packet %x %x, want %x %x", got.ID, got.Payload, kind, want)
		}
	}
	publish(world.LivingDamageChange{Entity: cow, Source: "lightning_bolt"})
	expect(proto.SDamageEvent, proto.NewWriter().VarInt(id).VarInt(24).VarInt(0).VarInt(0).Bool(false).Payload())
	metadata := func(flags byte) []byte {
		return proto.NewWriter().VarInt(id).U8(0).VarInt(0).U8(flags).U8(9).VarInt(3).F32(5).U8(0xff).Payload()
	}
	expect(proto.SEntityMetadata, metadata(0))
	cow.OnFire = true
	publish(world.LivingDamageChange{Entity: cow})
	expect(proto.SEntityMetadata, metadata(1))
	publish(world.LivingDamageChange{Entity: cow, Removed: true})
	expect(proto.SEntityDestroy, destroyPayload([]int32{id}))
	if len(other.Out) != 0 || s.generatedEntities[key] != nil {
		t.Fatal("damage visibility or removal tracking differs")
	}
}

func TestLightningConversionPackets(t *testing.T) {
	s, p := interactionServer(t)
	p.SetPlayReady(true)
	p.MarkChunkSent(0, 0)
	dim := s.World.DimensionName()
	players := []*player.Player{p}
	pos := [3]float64{8.5, -59, 8.5}
	old := world.WorldEntity{UUID: [16]byte{3}, RuntimeID: 31, Type: "minecraft:pig", Position: pos}
	spawn := func(e world.WorldEntity) {
		s.publishLivingDamage(players, dim, world.LivingDamageChange{Entity: e, Spawned: true})
	}
	spawn(old)
	for len(p.Out) > 0 {
		<-p.Out
	}
	sword := nbt.Object{"id": {Type: nbt.String, Value: "minecraft:golden_sword"}, "count": {Type: nbt.Int, Value: int32(1)}}
	piglin := world.WorldEntity{UUID: [16]byte{4}, RuntimeID: 32, Type: "minecraft:zombified_piglin", Position: pos, Data: nbt.Object{
		"HandItems": {Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{{Type: nbt.Compound, Value: sword}}},
		"NoAI":      {Type: nbt.Byte, Value: int8(1)}, "IsBaby": {Type: nbt.Byte, Value: int8(1)}, "Health": {Type: nbt.Float, Value: float32(20)},
		"CustomName": {Type: nbt.String, Value: `"Storm"`}, "CustomNameVisible": {Type: nbt.Byte, Value: int8(1)},
	}}
	spawn(piglin)
	s.publishLivingDamage(players, dim, world.LivingDamageChange{Entity: old, Removed: true})
	expect := func(id int32) []byte {
		t.Helper()
		if len(p.Out) == 0 {
			t.Fatalf("missing packet %x", id)
		}
		pkt := <-p.Out
		if pkt.ID != id {
			t.Fatalf("packet %x, want %x", pkt.ID, id)
		}
		return pkt.Payload
	}
	first := expect(proto.SSpawnEntity)
	if !bytes.HasPrefix(first, proto.NewWriter().VarInt(32).UUID(piglin.UUID).VarInt(127).Payload()) {
		t.Fatal("replacement spawn identity", first)
	}
	// Captured from the named juvenile in a frozen 1.21.1 dedicated server.
	if got := hex.EncodeToString(expect(proto.SEntityMetadata)); got != "2002060108000553746f726d030801090341a000000f0001100801ff" {
		t.Fatal("replacement metadata", got)
	}
	expect(proto.SSetEquipment)
	if got := expect(proto.SEntityDestroy); !bytes.Equal(got, destroyPayload([]int32{31})) {
		t.Fatal("wrong actor removed", got)
	}
	cow := world.WorldEntity{UUID: [16]byte{5}, RuntimeID: 33, Type: "minecraft:mooshroom", Position: pos}
	spawn(cow)
	for len(p.Out) > 0 {
		<-p.Out
	}
	change := world.LivingDamageChange{Entity: cow, MooshroomVariant: "red", SoundSeed: 123}
	publishMooshroomSound(players, dim, change)
	s.publishLivingDamage(players, dim, change)
	if got := expect(proto.SSoundEffect); !bytes.Equal(got, proto.NewWriter().VarInt(827).VarInt(6).I32(68).I32(-472).I32(68).F32(2).F32(1).I64(123).Payload()) {
		t.Fatal("mooshroom sound", got)
	}
	if got := hex.EncodeToString(expect(proto.SEntityMetadata)); got != "21110403726564ff" {
		t.Fatal("brown to red metadata", got)
	}
}
