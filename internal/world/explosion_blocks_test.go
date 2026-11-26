package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
)

func TestCreeperBlastBlocksAgainstDedicatedServer(t *testing.T) {
	type drop struct {
		Item  string
		Count int32
		Pos   [3]float64
	}
	var ref struct {
		Initial [][4]int32
		Cases   []struct {
			Seed, Next  int64
			Power       float32
			Mode        int
			Interaction int32
			Affected    int
			Hash        string
			Destroyed   []int
			Items       []drop
		}
	}
	raw, err := os.ReadFile("../../testdata/worldgen/creeper_blast_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &ref); err != nil || len(ref.Cases) != 16 {
		t.Fatalf("native scenes: %v", err)
	}
	for _, row := range ref.Cases {
		t.Run(fmt.Sprintf("%d_power%g_mode%d", row.Seed, row.Power, row.Mode), func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(1234)
			if err := CreateFlatWorld(dir, &seed); err != nil {
				t.Fatal(err)
			}
			w, err := Open(noViewers{}, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			for name, value := range map[string]bool{"mobGriefing": row.Mode != 0, "mobExplosionDropDecay": row.Mode != 1, "doTileDrops": row.Mode != 3} {
				if err := w.SetExplosionRule(name, value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := w.LoadChunk(0, 0); err != nil {
				t.Fatal(err)
			}
			if err := w.fallingEntityOperation(0, 0, func(_ *Region, ch *Chunk) error {
				for _, v := range ref.Initial {
					ch.SetBlock(int(v[0]), int(v[2]), v[1], uint32(v[3]))
				}
				mob := WorldEntity{Type: "minecraft:creeper", Position: [3]float64{8.5, 200, 8.5}, UUID: [16]byte{7}, Data: nbt.Object{
					"Health": {Type: nbt.Float, Value: float32(20)}, "Fuse": {Type: nbt.Short, Value: int16(1)}, "ignited": byteTag(1),
				}}
				if row.Power == 6 {
					mob.Data["powered"] = byteTag(1)
				}
				ch.entities = []WorldEntity{mob}
				ch.indexLivingDamage()
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			w.tickRandom = worldgen.NewLegacyRandom(row.Seed)
			var event CreeperFuseEvent
			var items []drop
			collect := func(e WorldEntity) {
				stack, _ := e.Data.Object("Item")
				name, _ := stack.Text("id")
				n, _ := stack.Number("count")
				items = append(items, drop{name, int32(n), e.Position})
			}
			if err := w.tickCreepers(func(int32, int32) bool { return true }, func(c LivingDamageChange) {
				if c.Spawned {
					collect(c.Entity)
				}
				if c.Fuse != nil {
					event = *c.Fuse
				}
			}); err != nil {
				t.Fatal(err)
			}
			if !event.Exploded || event.Power != row.Power {
				t.Fatalf("terminal fuse %+v", event)
			}
			h := sha256.New()
			for _, cell := range event.Affected {
				for _, n := range []int32{cell.X, cell.Y, cell.Z} {
					binary.Write(h, binary.BigEndian, n)
				}
			}
			if len(event.Affected) != row.Affected || hex.EncodeToString(h.Sum(nil)) != row.Hash || event.BlockInteraction != row.Interaction {
				t.Fatalf("interaction %d, affected %d hash %x, want %d / %d / %s", event.BlockInteraction, len(event.Affected), h.Sum(nil), row.Interaction, row.Affected, row.Hash)
			}
			var destroyed []int
			for i, v := range ref.Initial {
				id, err := w.BlockAt(v[0], v[1], v[2])
				if err != nil {
					t.Fatal(err)
				}
				if id == 0 {
					destroyed = append(destroyed, i)
				} else if id != uint32(v[3]) {
					t.Errorf("cell %d became %d", i, id)
				}
			}
			if !slices.Equal(destroyed, row.Destroyed) {
				t.Errorf("destroyed %v, native %v", destroyed, row.Destroyed)
			}
			if !slices.Equal(items, row.Items) {
				t.Errorf("spawned %+v, native %+v", items, row.Items)
			}
			if next := int64(w.tickRandom.NextLong()); next != row.Next {
				t.Errorf("next RNG %d, native %d", next, row.Next)
			}
			if row.Seed == 17 && row.Power == 6 && row.Mode == 2 {
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				loaded, err := Open(noViewers{}, 1, dir)
				if err != nil {
					t.Fatal(err)
				}
				defer loaded.Close()
				if _, err := loaded.LoadChunk(0, 0); err != nil {
					t.Fatal(err)
				}
				entities, err := loaded.EntitiesInChunk(0, 0)
				if err != nil {
					t.Fatal(err)
				}
				items = nil
				for _, e := range entities {
					if e.Type == "minecraft:item" {
						collect(e)
					}
				}
				if !slices.Equal(items, row.Items) {
					t.Errorf("reloaded items %+v", items)
				}
				for _, i := range row.Destroyed {
					v := ref.Initial[i]
					if id, err := loaded.BlockAt(v[0], v[1], v[2]); err != nil || id != 0 {
						t.Fatalf("reloaded cell %v: %d %v", v, id, err)
					}
				}
			}
		})
	}
}

func TestBlastChestContentsAgainstDedicatedServer(t *testing.T) {
	type drop struct {
		Item  string
		Count int32
		Pos   [3]float64
	}
	var ref struct {
		Containers []struct {
			Drops bool
			Items []drop
			Next  int64
		}
	}
	data, err := os.ReadFile("../../testdata/worldgen/creeper_blast_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &ref); err != nil || len(ref.Containers) != 2 {
		t.Fatalf("chest fixture: %v", err)
	}
	for _, row := range ref.Containers {
		t.Run(fmt.Sprint(row.Drops), func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(1234)
			if err := CreateFlatWorld(dir, &seed); err != nil {
				t.Fatal(err)
			}
			w, err := Open(noViewers{}, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			if err := w.SetExplosionRule("doTileDrops", row.Drops); err != nil {
				t.Fatal(err)
			}
			if err := w.SetExplosionRule("mobExplosionDropDecay", false); err != nil {
				t.Fatal(err)
			}
			if _, err := w.LoadChunk(0, 0); err != nil {
				t.Fatal(err)
			}
			id, err := BlockStateID("minecraft:chest", map[string]string{"facing": "north", "type": "single", "waterlogged": "false"})
			if err != nil {
				t.Fatal(err)
			}
			if err := w.fallingEntityOperation(0, 0, func(_ *Region, ch *Chunk) error {
				ch.SetBlock(8, 8, 200, id)
				stack := func(slot int8, name string, count int32) nbt.Tag {
					return nbt.Tag{Type: nbt.Compound, Value: nbt.Object{
						"Slot": byteTag(slot), "id": {Type: nbt.String, Value: name}, "count": {Type: nbt.Int, Value: count}}}
				}
				be := nbt.Object{"id": {Type: nbt.String, Value: "minecraft:chest"}, "x": {Type: nbt.Int, Value: int32(8)},
					"y": {Type: nbt.Int, Value: int32(200)}, "z": {Type: nbt.Int, Value: int32(8)}, "Items": {Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{stack(1, "minecraft:diamond", 40), stack(7, "minecraft:apple", 2)}}}
				root, _ := ch.anvilRoot.Object()
				root["block_entities"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{{Type: nbt.Compound, Value: be}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			w.tickRandom = worldgen.NewLegacyRandom(17)
			change := LivingDamageChange{Entity: WorldEntity{Position: [3]float64{8.5, 200, 8.5}}, Fuse: &CreeperFuseEvent{Power: 3, Exploded: true}}
			var items []drop
			if err := w.creeperBlast(change, func(c LivingDamageChange) {
				stack, _ := c.Entity.Data.Object("Item")
				name, _ := stack.Text("id")
				n, _ := stack.Number("count")
				items = append(items, drop{name, int32(n), c.Entity.Position})
			}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(items, row.Items) {
				t.Errorf("contents %+v, native %+v", items, row.Items)
			}
			if next := int64(w.tickRandom.NextLong()); next != row.Next {
				t.Errorf("next RNG %d native %d", next, row.Next)
			}
		})
	}
}
