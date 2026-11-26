package server

import (
	"atlas/internal/anvil"
	"atlas/internal/nbt"
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestLightningCreeperPacketsAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/lightning_creepers_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Name, Spawn, Reloaded string
		EntityType            int32
		Ticks                 []struct {
			Powered, Burning bool
			Health           float32
			Dirty, Tracking  string
		}
	}
	if err := json.Unmarshal(raw, &scenes); err != nil || len(scenes) != 6 {
		t.Fatalf("creeper reference: %d scenes, %v", len(scenes), err)
	}
	for _, scene := range scenes {
		t.Run(scene.Name, func(t *testing.T) {
			s, p := interactionServer(t)
			p.SetPlayReady(true)
			p.MarkChunkSent(0, 0)
			untracked := player.New("Hidden", [16]byte{8}, 8, nil)
			untracked.SetPlayReady(true)
			players := []*player.Player{p, untracked}
			dim := s.World.DimensionName()
			e := world.WorldEntity{Type: "minecraft:creeper", UUID: [16]byte{7}, RuntimeID: 77, Position: [3]float64{8, 70, 8}, Data: nbt.Object{"Health": {Type: nbt.Float, Value: float32(20)}, "NoAI": {Type: nbt.Byte, Value: int8(1)}}}
			if scene.Name == "precharged" {
				e.Data["powered"] = nbt.Tag{Type: nbt.Byte, Value: int8(1)}
			}
			if id, ok := generatedEntityTypes[e.Type]; !ok || id != scene.EntityType {
				t.Fatalf("creeper type %d, native %d", id, scene.EntityType)
			}
			s.publishLivingDamage(players, dim, world.LivingDamageChange{Entity: e, Spawned: true})
			expect := func(kind int32) []byte {
				t.Helper()
				if len(p.Out) == 0 {
					t.Fatalf("missing packet %x", kind)
				}
				got := <-p.Out
				if got.ID != kind {
					t.Fatalf("packet %x, want %x", got.ID, kind)
				}
				return got.Payload
			}
			expect(proto.SSpawnEntity)
			if got := hex.EncodeToString(expect(proto.SEntityMetadata)); got != scene.Spawn {
				t.Fatalf("spawn metadata %s, native %s", got, scene.Spawn)
			}
			for i, row := range scene.Ticks {
				prev := e
				e.Data = maps.Clone(e.Data)
				e.Revision++
				e.OnFire = row.Burning
				e.Data["Health"] = nbt.Tag{Type: nbt.Float, Value: row.Health}
				if row.Powered {
					e.Data["powered"] = nbt.Tag{Type: nbt.Byte, Value: int8(1)}
				}
				change := world.LivingDamageChange{Entity: e, CreeperCharged: !prev.IsChargedCreeper() && e.IsChargedCreeper()}
				if prev.Data["Health"].Value != row.Health {
					change.Source = "lightning_bolt"
				}
				if change.CreeperCharged || change.Source != "" || prev.OnFire != e.OnFire {
					s.publishLivingDamage(players, dim, change)
				}
				if change.Source != "" {
					expect(proto.SDamageEvent)
				}
				if row.Dirty != "" {
					if got := hex.EncodeToString(expect(proto.SEntityMetadata)); got != row.Dirty {
						t.Fatalf("tick %d: dirty metadata %s, native %s", i+1, got, row.Dirty)
					}
				}
				if len(p.Out) != 0 || len(untracked.Out) != 0 {
					t.Fatalf("tick %d: duplicate or untracked packet", i+1)
				}
				if got := hex.EncodeToString(generatedEntityMetadata(77, e)); got != row.Tracking {
					t.Fatalf("tick %d: new tracking metadata %s, native %s", i+1, got, row.Tracking)
				}
			}
			checkSavedCreeperTracking(t, e.Data, scene.Reloaded)
		})
	}
}

func checkSavedCreeperTracking(t *testing.T, data nbt.Object, native string) {
	t.Helper()
	s, p, dir := dimensionTestServer(t)
	data = maps.Clone(data)
	data["id"] = nbt.Tag{Type: nbt.String, Value: "minecraft:creeper"}
	data["UUID"] = nbt.Tag{Type: nbt.IntArray, Value: []int32{0x07000000, 0, 0, 0}}
	data["Pos"] = nbt.Tag{Type: nbt.List, ListType: nbt.Double, Value: []nbt.Tag{{Type: nbt.Double, Value: 8.0}, {Type: nbt.Double, Value: 70.0}, {Type: nbt.Double, Value: 8.0}}}
	data["Rotation"] = nbt.Tag{Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: float32(0)}, {Type: nbt.Float, Value: float32(0)}}}
	root := nbt.Object{
		"DataVersion": {Type: nbt.Int, Value: int32(3955)},
		"Position":    {Type: nbt.IntArray, Value: []int32{0, 0}},
		"Entities":    {Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{{Type: nbt.Compound, Value: data}}},
	}
	if err := anvil.WriteChunk(filepath.Join(dir, "entities"), 0, 0, "", nbt.Tag{Type: nbt.Compound, Value: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.World.LoadChunk(0, 0); err != nil {
		t.Fatal(err)
	}
	p.MarkChunkSent(0, 0)
	s.updateGeneratedEntityVisibility(p)
	if len(p.Out) < 2 {
		t.Fatal("missing spawn or metadata for saved creeper")
	}
	spawn, meta := <-p.Out, <-p.Out
	sr, mr := proto.NewReader(spawn.Payload), proto.NewReader(meta.Payload)
	id, err := sr.VarInt()
	metaID, metaErr := mr.VarInt()
	if spawn.ID != proto.SSpawnEntity || meta.ID != proto.SEntityMetadata || err != nil || metaErr != nil || id != metaID {
		t.Fatal("invalid spawn and metadata pair for saved creeper")
	}
	// The native probe encodes the same fields with a fixed runtime ID of 77.
	got := hex.EncodeToString(proto.NewWriter().VarInt(77).Raw(mr.Rest()).Payload())
	if got != native {
		t.Fatalf("saved creeper's initial tracking metadata %s, native %s", got, native)
	}
}
