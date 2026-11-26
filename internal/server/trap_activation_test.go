package server

import (
	"atlas/internal/anvil"
	"atlas/internal/nbt"
	"atlas/internal/proto"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSkeletonTrapActivationPacketsAgainstVanilla(t *testing.T) {
	s, p, dir := dimensionTestServer(t)
	reserveTestEntityIDs(100)
	native, err := os.ReadFile("../../testdata/protocol/skeleton_trap_activation_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Name  string
		Ticks []struct {
			Spawns []int32
			Mounts [][2]int
		}
	}
	if err := json.Unmarshal(native, &scenes); err != nil {
		t.Fatal(err)
	}
	var wantSpawns []int32
	var wantMounts [][2]int
	for _, scene := range scenes {
		if scene.Name == "first" {
			wantSpawns, wantMounts = scene.Ticks[0].Spawns, scene.Ticks[0].Mounts
		}
	}
	if len(wantSpawns) != 8 || len(wantMounts) != 4 {
		t.Fatal("missing native activation")
	}
	template, err := os.ReadFile("../world/skeleton_trap_1.21.1.bin")
	if err != nil {
		t.Fatal(err)
	}
	var horses []nbt.Tag
	for i, x := range []float64{2, 15} {
		tag, _, err := nbt.ParseNetwork(template)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := tag.Object()
		data["id"] = nbt.Tag{Type: nbt.String, Value: "minecraft:skeleton_horse"}
		data["UUID"] = nbt.Tag{Type: nbt.IntArray, Value: []int32{12, 34, 56, int32(i + 1)}}
		data["Pos"] = nbt.Tag{Type: nbt.List, ListType: nbt.Double, Value: []nbt.Tag{{Type: nbt.Double, Value: x}, {Type: nbt.Double, Value: float64(-59)}, {Type: nbt.Double, Value: float64(8)}}}
		data["Rotation"] = nbt.Tag{Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: float32(0)}, {Type: nbt.Float, Value: float32(0)}}}
		data["SkeletonTrapTime"] = nbt.Tag{Type: nbt.Int, Value: int32(i * 18000)}
		horses = append(horses, tag)
	}
	root := nbt.Tag{Type: nbt.Compound, Value: nbt.Object{"DataVersion": {Type: nbt.Int, Value: int32(3955)}, "Position": {Type: nbt.IntArray, Value: []int32{0, 0}}, "Entities": {Type: nbt.List, ListType: nbt.Compound, Value: horses}}}
	if err := anvil.WriteChunk(filepath.Join(dir, "entities"), 0, 0, "", root); err != nil {
		t.Fatal(err)
	}
	if err := s.World.SetMobSpawning(false); err != nil {
		t.Fatal(err)
	}
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			if _, err := s.World.LoadChunk(x, z); err != nil {
				t.Fatal(err)
			}
		}
	}
	p.SetPos(2, -59, 8, 0, 0, true)
	p.MarkChunkSent(0, 0)
	s.updateGeneratedEntityVisibility(p)
	var original, expired int32
	for _, v := range s.generatedEntities {
		if v.snapshot.Position[0] == 2 {
			original = v.id
		} else if v.snapshot.Position[0] == 15 {
			expired = v.id
		}
	}
	if original == 0 || expired == 0 {
		t.Fatal("missing initial horses", original, expired)
	}
	for len(p.Out) > 0 {
		<-p.Out
	}
	if err := s.TickWorlds(); err != nil {
		t.Fatal(err)
	}
	index := map[int32]int{original: 0}
	var spawns []int32
	var mounts [][2]int
	removed := false
	for len(p.Out) > 0 {
		packet := <-p.Out
		r := proto.NewReader(packet.Payload)
		switch packet.ID {
		case proto.SSpawnEntity:
			id, _ := r.VarInt()
			_, _ = r.UUID()
			typ, err := r.VarInt()
			if _, exists := index[id]; exists || err != nil {
				t.Fatal("duplicate or malformed spawn", id, err)
			}
			spawns = append(spawns, typ)
			index[id] = len(spawns)
		case proto.SSetPassengers:
			vehicle, _ := r.VarInt()
			count, _ := r.VarInt()
			child, err := r.VarInt()
			v, vOK := index[vehicle]
			c, cOK := index[child]
			if count != 1 || err != nil || !vOK || !cOK {
				t.Fatal("attachment before spawn", vehicle, child, count, err)
			}
			mounts = append(mounts, [2]int{v, c})
		case proto.SEntityDestroy:
			count, _ := r.VarInt()
			id, err := r.VarInt()
			if count != 1 || id != expired || err != nil {
				t.Fatal("unexpected removal", id, count, err)
			}
			removed = true
		}
	}
	if !slices.Equal(spawns, wantSpawns) || !slices.Equal(mounts, wantMounts) || !removed || p.IsVisible(expired) {
		t.Fatalf("spawn order %v mounts %v expired %v", spawns, mounts, removed)
	}
}
