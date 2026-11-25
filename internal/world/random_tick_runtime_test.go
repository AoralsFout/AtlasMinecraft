package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"slices"
	"strconv"
	"testing"
)

func TestPlayerTickFellsWaterloggedLeavesAndPersistsDrops(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(1234)
			if err := CreateFlatWorld(dir, &seed); err != nil {
				t.Fatal(err)
			}
			w, err := Open(noViewers{}, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := w.Close(); err != nil {
					t.Error(err)
				}
			})
			// This fixture fixes the number of eligible chunks and RNG draws.
			if err := w.SetSpawnChunkRadius(0); err != nil {
				t.Fatal(err)
			}
			v := w.store.vanilla
			v.flat.layers = nil
			for x := int32(-2); x <= 2; x++ {
				for z := int32(-2); z <= 2; z++ {
					if _, err := w.LoadChunk(x, z); err != nil {
						t.Fatal(err)
					}
				}
			}
			root, _ := v.root.Object()
			data, _ := root.Object("Data")
			rules, _ := data.Object("GameRules")
			setRule := func(key, value string) {
				v.mu.Lock()
				defer v.mu.Unlock()
				rules[key] = nbt.Tag{Type: nbt.String, Value: value}
			}
			setRule("doTileDrops", strconv.FormatBool(enabled))
			setRule("randomTickSpeed", "0")
			id := func(name string, props map[string]string) uint32 {
				v, err := BlockStateID(name, props)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			trunk := id("oak_log", nil)
			if err := w.SetBlock(7, 69, 8, trunk); err != nil {
				t.Fatal(err)
			}
			props := map[string]string{"distance": "1", "waterlogged": "true"}
			if err := w.SetBlock(8, 69, 8, id("oak_leaves", props)); err != nil {
				t.Fatal(err)
			}
			if err := w.TryPlaceBlock(BlockPosition{7, 69, 8}, trunk, StateAir, nil); err != nil {
				t.Fatal(err)
			}
			players := []SimulationPlayer{{X: 8, Z: 8}}
			tick := func() {
				t.Helper()
				if err := w.TickPlayers(players, 0); err != nil {
					t.Fatal(err)
				}
			}
			check := func(want uint32) {
				t.Helper()
				got, err := w.BlockAt(8, 69, 8)
				if err != nil || got != want {
					t.Fatalf("state %d want %d, err %v", got, want, err)
				}
			}
			tick()
			props["distance"] = "7"
			check(id("oak_leaves", props))
			// Native oak roll 121 yields a sapling and one stick, in this order.
			for i := 0; i < 121; i++ {
				if _, err := w.randomSequences.leafDrops("minecraft:oak_leaves"); err != nil {
					t.Fatal(err)
				}
			}
			setRule("randomTickSpeed", "1")
			players[0].Spectator = true
			w.randomPosition = 0
			tick()
			if w.randomPosition != 0 {
				t.Fatalf("spectator triggered a coordinate draw")
			}
			players[0].Spectator = false
			// Invert Java's position recurrence for local (8,5,8).
			bits := uint32(8 | 8<<8 | 5<<16)
			w.randomPosition = ((bits << 2) - 1013904223) * 0xaaaaaaab
			w.tickRandom = worldgen.NewLegacyRandom(13)
			expected := worldgen.NewLegacyRandom(13)
			for n := int32(9); n > 1; n-- {
				expected.NextIntBound(n)
			}
			if expected.NextIntBound(48) == 0 {
				t.Fatal("seed must skip precipitation")
			}
			tick()
			check(id("water", nil))
			draws := 6 // both stacks draw three coordinates regardless of doTileDrops
			var want []string
			if enabled {
				draws = 10
				want = []string{"minecraft:oak_sapling", "minecraft:stick"}
			}
			for i := 0; i < draws; i++ {
				expected.NextDouble()
			}
			if got := w.tickRandom.State(); got != expected.State() {
				t.Fatalf("world RNG %d want %d", got, expected.State())
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			w, err = Open(noViewers{}, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			check(id("water", nil))
			entities, err := w.EntitiesInChunk(0, 0)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, e := range entities {
				item, _ := e.Data.Object("Item")
				name, _ := item.Text("id")
				count, _ := item.Number("count")
				delay, _ := e.Data.Number("PickupDelay")
				if count != 1 || delay != 10 {
					t.Fatalf("invalid saved item %s: count %d delay %d", name, count, delay)
				}
				got = append(got, name)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("saved drops %v want %v", got, want)
			}
		})
	}
}
