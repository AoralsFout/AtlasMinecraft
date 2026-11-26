package server

import (
	"atlas/internal/nbt"
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"bytes"
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
