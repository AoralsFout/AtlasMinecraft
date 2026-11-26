package server

import (
	"atlas/internal/nbt"
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"encoding/hex"
	"slices"
	"testing"
)

func TestSkeletonTrapSpawnPacketsAndGameRule(t *testing.T) {
	s, near := interactionServer(t)
	for _, value := range []string{"false", "true"} {
		if got := s.commandGameRule([]string{"doMobSpawning", value}); got != "Gamerule doMobSpawning is now set to: "+value {
			t.Fatal(got)
		}
		if got := s.commandGameRule([]string{"doMobSpawning"}); got != "Gamerule doMobSpawning is currently set to: "+value {
			t.Fatal(got)
		}
	}
	near.SetPlayReady(true)
	near.MarkChunkSent(0, 0)
	distant := player.New("Distant", [16]byte{3}, 3, nil)
	distant.SetPlayReady(true)
	distant.MarkChunkSent(0, 0)
	distant.SetPos(200, -58, 8, 0, 0, true)
	horse := world.WorldEntity{UUID: [16]byte{7}, Type: world.SkeletonHorseEntity, Position: [3]float64{8, -58, 8}, Data: nbt.Object{"Health": {Type: nbt.Float, Value: float32(15)}}}
	bolt := world.WorldEntity{UUID: [16]byte{8}, Type: world.LightningEntity, Position: [3]float64{8.5, -58, 8.5}}
	s.publishTrapStrike([]*player.Player{near, distant}, s.World.DimensionName(), horse, bolt)
	check := func(p *player.Player, want []int32) {
		t.Helper()
		var got []int32
		for len(p.Out) > 0 {
			pkt := <-p.Out
			if pkt.ID != proto.SSpawnEntity {
				continue
			}
			r := proto.NewReader(pkt.Payload)
			_, _ = r.VarInt()
			uuid, _ := r.UUID()
			kind, err := r.VarInt()
			x, _ := r.F64()
			y, _ := r.F64()
			z, errZ := r.F64()
			expected := bolt
			if kind == 92 {
				expected = horse
			}
			if err != nil || errZ != nil || uuid != expected.UUID || [3]float64{x, y, z} != expected.Position {
				t.Fatalf("unexpected entity %d at %v %v %v", kind, x, y, z)
			}
			got = append(got, kind)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s spawn order %v, want %v", p.Name, got, want)
		}
	}
	check(near, []int32{92, 64})
	check(distant, []int32{64})
}

func TestLightningTrackingMatchesVanillaSpawnAndRemoval(t *testing.T) {
	s, first := interactionServer(t)
	first.SetPlayReady(true)
	first.MarkChunkSent(0, 0)
	second := player.New("Pending", [16]byte{2}, 2, nil)
	second.SetPlayReady(true)
	nether := player.New("Nether", [16]byte{3}, 3, nil)
	nether.SetPlayReady(true)
	nether.SetInitialDimension("minecraft:the_nether")
	nether.MarkChunkSent(0, 0)
	players := []*player.Player{first, second, nether}
	entity := world.WorldEntity{UUID: [16]byte{7}, Type: world.LightningEntity, Position: [3]float64{8.5, -58, 8.5}}
	// Block placement may hold entityMu throughout the chunk phase.
	s.entityMu.Lock()
	register := s.publishLightningSpawn(players, s.World.DimensionName(), entity)
	s.entityMu.Unlock()
	register()
	if len(first.Out) != 1 || len(second.Out) != 0 || len(nether.Out) != 0 {
		t.Fatal("unexpected lightning recipients")
	}
	packet := <-first.Out
	r := proto.NewReader(packet.Payload)
	id, err := r.VarInt()
	uuid, uuidErr := r.UUID()
	if packet.ID != proto.SSpawnEntity || err != nil || uuidErr != nil || uuid != entity.UUID || !first.IsVisible(id) {
		t.Fatalf("invalid spawn packet %v", packet)
	}
	// Type and remaining bytes captured from a natural strike on Java 1.21.1.
	const vanilla = "404021000000000000c04d000000000000402100000000000000000000000000000000"
	body, _ := r.Bytes(r.Remaining())
	if got := hex.EncodeToString(body); got != vanilla {
		t.Fatalf("spawn body %s, want %s", got, vanilla)
	}
	s.updateGeneratedEntityVisibility(first)
	if !first.IsVisible(id) {
		t.Fatal("generated entity refresh removed live bolt")
	}
	s.publishTickingEntities(s.World, players)
	if len(first.Out) != 1 || first.IsVisible(id) || s.trackedTicking != 0 {
		t.Fatal("expired bolt still tracked or missing removal")
	}
	packet = <-first.Out
	r = proto.NewReader(packet.Payload)
	n, err := r.VarInt()
	removed, idErr := r.VarInt()
	if packet.ID != proto.SEntityDestroy || err != nil || idErr != nil || n != 1 || removed != id || r.Remaining() != 0 {
		t.Fatalf("invalid destroy packet %v", packet)
	}
}

func TestFallingEntityVisibilityWaitsForTerrainAndSharesMotion(t *testing.T) {
	s, first := interactionServer(t)
	reserveTestEntityIDs(10)
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
		s.publishTickingEntities(s.World, s.Hub.Snapshot())
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
