package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"testing"
)

func TestFallingEntityVisibilityWaitsForTerrainAndSharesMotion(t *testing.T) {
	s, first := interactionServer(t)
	s.nextEID.Store(10)
	first.SetPlayReady(true)
	first.MarkChunkSent(0, 0)
	second := player.New("Second", [16]byte{2}, 2, nil)
	second.SetPlayReady(true)
	if !s.Hub.Join(second) {
		t.Fatal("second player could not join")
	}
	if err := s.World.SetBlock(8, -40, 8, stateID(t, "sand", nil)); err != nil {
		t.Fatal(err)
	}
	center := func(x, z int32) bool { return x == 0 && z == 0 }
	tick := func() {
		t.Helper()
		if err := s.World.Tick(world.TickChunks{Blocks: center, Entities: center}); err != nil {
			t.Fatal(err)
		}
		s.publishFallingEntities(s.World, s.Hub.Snapshot())
	}
	tick()
	tick()
	entities := s.World.FallingEntities()
	if len(entities) != 1 {
		t.Fatalf("expected one falling entity, got %d", len(entities))
	}
	key := generatedEntityKey{s.World.DimensionName(), entities[0].UUID}
	tracking := s.generatedEntities[key]
	if tracking == nil || tracking.viewers != 1 || !first.IsVisible(tracking.id) || second.IsVisible(tracking.id) {
		t.Fatal("falling entity should spawn only after terrain delivery")
	}
	second.MarkChunkSent(0, 0)
	tick()
	if !second.IsVisible(tracking.id) || tracking.viewers != 2 {
		t.Fatal("a later viewer should receive the same entity ID")
	}
	s.updateGeneratedEntityVisibility(first)
	for _, p := range []*player.Player{first, second} {
		for len(p.Out) != 0 {
			<-p.Out
		}
	}
	tick()
	for _, p := range []*player.Player{first, second} {
		moved := false
		for len(p.Out) != 0 {
			packet := <-p.Out
			id, _ := proto.NewReader(packet.Payload).VarInt()
			moved = moved || packet.ID == proto.SEntityTeleport && id == tracking.id
		}
		if !moved {
			t.Fatalf("%s did not get the position update", p.Name)
		}
	}
	first.PruneChunks(10, 10, 4)
	tick()
	if first.IsVisible(tracking.id) || !second.IsVisible(tracking.id) || tracking.viewers != 1 {
		t.Fatal("pruning one view should remove only its falling entity")
	}
}
