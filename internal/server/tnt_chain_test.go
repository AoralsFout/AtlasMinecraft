package server

import (
	"atlas/internal/anvil"
	"atlas/internal/nbt"
	"atlas/internal/proto"
	"atlas/internal/world"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrimedTNTPacketsAgainstDedicatedServer(t *testing.T) {
	var ref struct {
		Packets struct{ Spawn, Metadata string }
	}
	raw, err := os.ReadFile("../../testdata/worldgen/tnt_chain_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	e := world.WorldEntity{
		UUID: [16]byte{7: 1, 15: 2}, Type: world.TNTEntity, Position: [3]float64{8.5, 200, 8.5},
		Data: nbt.Object{
			"fuse": {Type: nbt.Short, Value: int16(18)},
			"Motion": {Type: nbt.List, ListType: nbt.Double, Value: []nbt.Tag{
				{Type: nbt.Double, Value: -.01}, {Type: nbt.Double, Value: float64(float32(.2))}, {Type: nbt.Double, Value: .015},
			}},
		},
	}
	if got := hex.EncodeToString(generatedEntitySpawnPayload(77, e)); got != ref.Packets.Spawn {
		t.Errorf("TNT spawn %s, native %s", got, ref.Packets.Spawn)
	}
	if got := hex.EncodeToString(generatedEntityMetadata(77, e)); got != ref.Packets.Metadata {
		t.Errorf("TNT metadata %s, native %s", got, ref.Packets.Metadata)
	}
}

func TestServerWorldTickPublishesPrimedTNTAcrossChunks(t *testing.T) {
	s, p, dir := dimensionTestServer(t)
	s.Cfg.SimulationDistance = 2
	p.SetInitialGameMode(0)
	p.SetPos(15.5, 200, 8.5, 0, 0, false)
	data := nbt.Object{
		"id":   {Type: nbt.String, Value: world.TNTEntity},
		"UUID": {Type: nbt.IntArray, Value: []int32{0, 0, 0, 1}},
		"Pos": {Type: nbt.List, ListType: nbt.Double, Value: []nbt.Tag{
			{Type: nbt.Double, Value: 15.9}, {Type: nbt.Double, Value: 200.0}, {Type: nbt.Double, Value: 8.5},
		}},
		"Motion": {Type: nbt.List, ListType: nbt.Double, Value: []nbt.Tag{
			{Type: nbt.Double, Value: .25}, {Type: nbt.Double, Value: 0.0}, {Type: nbt.Double, Value: 0.0},
		}},
		"Rotation": {Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: float32(0)}, {Type: nbt.Float, Value: float32(0)}}},
		"fuse":     {Type: nbt.Short, Value: int16(2)},
	}
	root := nbt.Object{
		"DataVersion": {Type: nbt.Int, Value: int32(3955)},
		"Position":    {Type: nbt.IntArray, Value: []int32{0, 0}},
		"Entities":    {Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{{Type: nbt.Compound, Value: data}}},
	}
	if err := anvil.WriteChunk(filepath.Join(dir, "entities"), 0, 0, "", nbt.Tag{Type: nbt.Compound, Value: root}); err != nil {
		t.Fatal(err)
	}
	for x := int32(-2); x <= 3; x++ {
		for z := int32(-2); z <= 2; z++ {
			if _, err := s.World.LoadChunk(x, z); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.World.TickScheduled(func(x, z int32) bool { return (x == 0 || x == 1) && z == 0 }); err != nil {
		t.Fatal(err)
	}
	p.MarkChunkSent(0, 0)
	p.MarkChunkSent(1, 0)
	s.updateGeneratedEntityVisibility(p)
	id := s.generatedViews[p][generatedEntityKey{s.World.DimensionName(), [16]byte{15: 1}}]
	if id == 0 {
		t.Fatal("saved TNT is not visible")
	}
	for len(p.Out) > 0 {
		<-p.Out
	}
	for tick := 1; tick <= 2; tick++ {
		if err := s.TickWorlds(); err != nil {
			t.Fatal(err)
		}
		var fuse, moved, blast, removed bool
		for len(p.Out) > 0 {
			packet := <-p.Out
			r := proto.NewReader(packet.Payload)
			switch packet.ID {
			case proto.SEntityMetadata:
				entity, _ := r.VarInt()
				fuse = fuse || entity == id && hex.EncodeToString(r.Rest()) == "080101ff"
			case proto.SEntityTeleport:
				entity, _ := r.VarInt()
				x, _ := r.F64()
				moved = moved || entity == id && x > 16
			case proto.SExplosion:
				for range 3 {
					_, _ = r.F64()
				}
				power, _ := r.F32()
				blast = blast || power == 4
			case proto.SEntityDestroy:
				count, _ := r.VarInt()
				for range count {
					entity, _ := r.VarInt()
					if entity == id {
						if !blast {
							t.Fatalf("TNT removed before its explosion on tick %d", tick)
						}
						removed = true
					}
				}
			}
		}
		if fuse != (tick == 1) || moved != fuse || blast != (tick == 2) || removed != blast {
			t.Fatalf("tick %d fuse update %t moved %t explosion %t removed %t", tick, fuse, moved, blast, removed)
		}
		if tick == 1 {
			if err := s.World.Save(); err != nil {
				t.Fatal(err)
			}
			for cx, count := range []int{0, 1} {
				_, root, ok, err := anvil.ReadChunk(filepath.Join(dir, "entities"), int32(cx), 0)
				saved, _ := root.Object()
				entries, _ := saved["Entities"].Elements()
				if !ok || err != nil || len(entries) != count {
					t.Fatalf("saved chunk %d has %d entities, want %d: %v", cx, len(entries), count, err)
				}
				if count == 1 {
					entity, _ := entries[0].Object()
					if fuse, _ := entity.Number("fuse"); fuse != 1 {
						t.Fatalf("saved TNT fuse %d, want 1", fuse)
					}
				}
			}
		}
	}
}
