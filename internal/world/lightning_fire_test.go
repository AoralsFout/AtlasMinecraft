package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"encoding/json"
	"maps"
	"math"
	"os"
	"testing"
)

func TestLightningFireAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/lightning_fire_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Name       string
		Initial    [][4]int32
		Difficulty int8
		Visual     bool
		Position   [3]float64
		Seed       int64
		WorldSeed  int64
		Chunk      [2]int32
		Ticks      []struct {
			Enabled, Removed  bool
			ClearCenter       bool
			Life, Flashes     int32
			WorldRNG, BoltRNG uint64
			Fires             [][4]int32
		}
	}
	if err := json.Unmarshal(raw, &scenes); err != nil || len(scenes) != 17 {
		t.Fatalf("fire reference: %d cases, %v", len(scenes), err)
	}
	for _, scene := range scenes {
		t.Run(scene.Name, func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(1)
			if err := CreateFlatWorld(dir, &seed); err != nil {
				t.Fatal(err)
			}
			w, err := Open(noViewers{}, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := w.Close(); err != nil {
					t.Error(err)
				}
			}()
			w.weatherStopped = true
			if scene.Name == "rain_ticks" {
				w.meta.Raining = true
				w.levels.Rain = 1
			}
			root, _ := w.store.vanilla.root.Object()
			data, _ := root.Object("Data")
			data["Difficulty"] = nbt.Tag{Type: nbt.Byte, Value: scene.Difficulty}
			cx, cz := scene.Chunk[0], scene.Chunk[1]
			for x := cx - 1; x <= cx+1; x++ {
				for z := cz - 1; z <= cz+1; z++ {
					if _, err := w.LoadChunk(x, z); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, cell := range scene.Initial {
				if cell[3] != 0 {
					// The native probe loads a block snapshot with empty tick queues.
					// A player placement here would also schedule a tick for the water.
					if _, err := w.editBlock(setBlockMsg{x: cell[0], y: cell[1], z: cell[2], state: uint32(cell[3]), raw: true, flags: 2}); err != nil {
						t.Fatal(err)
					}
				}
			}
			center := func(x, z int32) bool { return x == cx && z == cz }
			w.tickRandom = worldgen.NewLegacyRandom(scene.WorldSeed)
			w.addLightning(WorldEntity{Type: LightningEntity, Position: scene.Position}, scene.Visual, scene.Seed)
			boltRNG := w.bolts[0].random
			fireBase, _ := BlockStateID("fire", nil)
			soul, _ := BlockStateID("soul_fire", nil)
			for i, row := range scene.Ticks {
				if row.ClearCenter {
					if err := w.SetBlock(int32(math.Floor(scene.Position[0])), int32(math.Floor(scene.Position[1])), int32(math.Floor(scene.Position[2])), StateAir); err != nil {
						t.Fatal(err)
					}
				}
				if w.FireTick() != row.Enabled {
					if err := w.SetFireTick(row.Enabled); err != nil {
						t.Fatal(err)
					}
				}
				if err := w.Tick(TickChunks{Blocks: center, Entities: center}); err != nil {
					t.Fatalf("tick %d: %v", i+1, err)
				}
				if len(w.bolts) == 0 != row.Removed || len(w.bolts) > 0 && (w.bolts[0].life != row.Life || w.bolts[0].flashes != row.Flashes) {
					t.Fatalf("tick %d: bolt lifetime differs", i+1)
				}
				if w.tickRandom.State() != row.WorldRNG || boltRNG.State() != row.BoltRNG {
					t.Fatalf("tick %d: RNG world/bolt %d/%d, want %d/%d", i+1, w.tickRandom.State(), boltRNG.State(), row.WorldRNG, row.BoltRNG)
				}
				want := make(map[[3]int32]uint32)
				for _, cell := range row.Fires {
					want[[3]int32{cell[0], cell[1], cell[2]}] = uint32(cell[3])
				}
				fires := make(map[[3]int32]uint32)
				err := w.fallingEntityOperation(cx, cz, func(_ *Region, ch *Chunk) error {
					for y := int32(66); y <= 79; y++ {
						for x := 0; x < 16; x++ {
							for z := 0; z < 16; z++ {
								id := ch.GetBlock(x, z, y)
								if id == soul || id == fireBase {
									fires[[3]int32{cx*16 + int32(x), y, cz*16 + int32(z)}] = id
								} else if id != 0 {
									s, _ := BlockStateByID(id)
									if s.Name == "minecraft:fire" {
										fires[[3]int32{cx*16 + int32(x), y, cz*16 + int32(z)}] = id
									}
								}
							}
						}
					}
					return nil
				})
				if err != nil || !maps.Equal(fires, want) {
					t.Fatalf("tick %d: fires %v, want %v, err %v", i+1, fires, want, err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			reloaded, err := Open(noViewers{}, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			w = reloaded
			last := scene.Ticks[len(scene.Ticks)-1]
			if w.FireTick() != last.Enabled {
				t.Fatal("doFireTick lost on restart")
			}
			for _, cell := range last.Fires {
				if id, err := w.BlockAt(cell[0], cell[1], cell[2]); err != nil || id != uint32(cell[3]) {
					t.Fatal("fire lost on restart", cell, id, err)
				}
			}
		})
	}
}

func TestLightningFireAcrossChunkBoundary(t *testing.T) {
	dir := t.TempDir()
	seed := int64(7)
	if err := CreateFlatWorld(dir, &seed); err != nil {
		t.Fatal(err)
	}
	w, err := Open(noViewers{}, 1, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for x := int32(0); x <= 1; x++ {
		if _, err := w.LoadChunk(x, 0); err != nil {
			t.Fatal(err)
		}
	}
	set := func(x, y int32, name string) {
		t.Helper()
		id, err := BlockStateID(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.SetBlock(x, y, 8, id); err != nil {
			t.Fatal(err)
		}
	}
	strike := func() {
		t.Helper()
		w.addLightning(WorldEntity{Type: LightningEntity, Position: [3]float64{15.5, 70, 8.5}}, false, 7)
		if err := w.Tick(TickChunks{Entities: func(x, z int32) bool { return true }}); err != nil {
			t.Fatal(err)
		}
	}
	set(16, 70, "oak_planks")
	strike()
	east, err := BlockStateID("fire", map[string]string{"east": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if id, err := w.BlockAt(15, 70, 8); err != nil || id != east {
		t.Fatal("fire did not attach across chunk border", id, err)
	}
	set(16, 70, "air")
	if id, err := w.BlockAt(15, 70, 8); err != nil || id != StateAir {
		t.Fatal("fire survived removal of its only fuel", id, err)
	}
	for x := int32(14); x <= 17; x++ {
		for y := int32(69); y <= 73; y++ {
			if x == 14 || x == 17 || y == 69 || y == 73 {
				set(x, y, "obsidian")
			}
		}
	}
	strike()
	portal, _ := BlockStateID("nether_portal", map[string]string{"axis": "x"})
	for x := int32(15); x <= 16; x++ {
		for y := int32(70); y <= 72; y++ {
			if id, err := w.BlockAt(x, y, 8); err != nil || id != portal {
				t.Fatalf("portal at %d,%d has %d: %v", x, y, id, err)
			}
		}
	}
}
