package world

import (
	"atlas/internal/worldgen"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"
)

func TestLightningChunkStreamAgainstVanilla(t *testing.T) {
	var scenes []struct {
		Name        string
		Chunk       [2]int32
		Initial     [][4]int32
		MobSpawning bool
		Position    [3]float64
		Row         referenceRandomTick
	}
	raw, err := os.ReadFile("../../testdata/worldgen/lightning_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &scenes); err != nil {
		t.Fatal(err)
	}
	for _, scene := range scenes {
		t.Run(scene.Name, func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(0)
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
			cx, cz := scene.Chunk[0], scene.Chunk[1]
			load := func() {
				t.Helper()
				for z := cz - 1; z <= cz+1; z++ {
					for x := cx - 1; x <= cx+1; x++ {
						if _, err := w.LoadChunk(x, z); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			w.store.vanilla.flat.layers = nil
			load()
			for _, cell := range scene.Initial {
				if err := w.SetBlock(cell[0], cell[1], cell[2], uint32(cell[3])); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.store.vanilla.saveGameRule("doMobSpawning", strconv.FormatBool(scene.MobSpawning), nil); err != nil {
				t.Fatal(err)
			}
			w.levels = WeatherLevels{Rain: 1, Thunder: 1}
			row := scene.Row
			w.randomPosition = uint32(row.LCGBefore)
			w.tickRandom = worldgen.NewLegacyRandom(row.Seed)
			var holder *chunkScheduledTicks
			if err := w.fallingEntityOperation(cx, cz, func(_ *Region, c *Chunk) error { holder = c.scheduledTicks; return nil }); err != nil {
				t.Fatal(err)
			}
			r := fluidRuntime{world: w, settings: w.fluidSettings()}
			var samples, precip [][3]int32
			err = r.randomTickChunk(holder, row.Speed, func(p worldgen.FeaturePosition, weather bool) {
				q := [3]int32{p.X, p.Y, p.Z}
				if weather {
					precip = append(precip, q)
				} else {
					samples = append(samples, q)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if bolts := w.LightningEntities(); len(bolts) != 1 || bolts[0].Position != scene.Position {
				t.Fatalf("spawned %v, want %v", bolts, scene.Position)
			}
			if !slices.Equal(samples, row.Samples[1:]) || !slices.Equal(precip, row.Precipitation) || int32(w.randomPosition) != row.LCGAfter || w.tickRandom.State() != row.RNGAfter {
				t.Fatalf("samples %v precipitation %v LCG %d RNG %d", samples, precip, int32(w.randomPosition), w.tickRandom.State())
			}
			eligible := func(x, z int32) bool { return x == cx && z == cz }
			tick := func() {
				t.Helper()
				if err := w.Tick(TickChunks{Blocks: eligible, Entities: eligible}); err != nil {
					t.Fatal(err)
				}
			}
			rod := scene.Initial[0]
			for i := 1; i <= 36; i++ {
				tick()
				id, err := w.BlockAt(rod[0], rod[1], rod[2])
				if err != nil {
					t.Fatal(err)
				}
				state, _ := BlockStateByID(id)
				if state.Properties["powered"] != strconv.FormatBool(i <= 8) {
					t.Fatalf("tick %d rod %v", i, state)
				}
				if i == 3 && scene.MobSpawning {
					if n, err := w.UnloadIdle(0); err != nil || n == 0 {
						t.Fatalf("unload: %d chunks, %v", n, err)
					}
					load()
					if len(w.LightningEntities()) != 0 {
						t.Fatal("bolt returned with reloaded chunk")
					}
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
					reopened, err := Open(noViewers{}, 1, dir)
					if err != nil {
						t.Fatal(err)
					}
					w = reopened
					load()
					if len(w.LightningEntities()) != 0 {
						t.Fatal("transient bolt persisted")
					}
				}
			}
			if len(w.LightningEntities()) != 0 {
				t.Fatal("bolt survived its flashes")
			}
		})
	}
}

func TestLightningRodRadiusAndLivingTargets(t *testing.T) {
	dir := t.TempDir()
	seed := int64(9)
	if err := CreateFlatWorld(dir, &seed); err != nil {
		t.Fatal(err)
	}
	w, err := Open(noViewers{}, 1, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			if _, err := w.LoadChunk(x, z); err != nil {
				t.Fatal(err)
			}
		}
	}
	r := fluidRuntime{world: w, settings: w.fluidSettings()}
	w.tickRandom = worldgen.NewLegacyRandom(19)
	origin := worldgen.FeaturePosition{X: 8, Y: 70, Z: 8}
	set := func(p worldgen.FeaturePosition, name string) {
		t.Helper()
		id, err := BlockStateID(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.SetBlock(p.X, p.Y, p.Z, id); err != nil {
			t.Fatal(err)
		}
	}
	ground := origin
	ground.Y--
	set(ground, "stone")
	check := func(want worldgen.FeaturePosition) {
		t.Helper()
		got, err := r.lightningTarget(origin)
		if err != nil || got != want {
			t.Fatalf("target %+v, want %+v: %v", got, want, err)
		}
	}
	check(origin)
	w.simulationPlayers = []SimulationPlayer{{X: 9.5, Y: 72, Z: 9.5}}
	playerPos := worldgen.FeaturePosition{X: 9, Y: 72, Z: 9}
	check(playerPos)
	roof := playerPos
	roof.Y++
	set(roof, "stone")
	check(origin)
	set(roof, "air")
	far := origin
	far.X += 128
	set(far, "lightning_rod")
	top := far
	top.Y++
	check(top)
	// WORLD_SURFACE rejects a rod even under a transparent, non-motion-blocking cover.
	cover := top
	cover.Y++
	set(cover, "glass")
	check(playerPos)
	set(cover, "air")
	set(far, "air")
	far.Y++
	set(far, "lightning_rod")
	check(playerPos)
}
