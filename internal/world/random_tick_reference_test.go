package world

import (
	"atlas/internal/light"
	"atlas/internal/registry"
	"atlas/internal/tickqueue"
	"atlas/internal/worldgen"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
)

type referenceRandomTick struct {
	PendingFarmland        *bool
	Tick, Speed, Light     int32
	Seed                   int64
	LCGBefore, LCGAfter    int32
	RNGAfter               uint64
	Edits                  [][4]int32
	Samples, Precipitation [][3]int32
	States                 []uint32
}
type randomTickScene struct {
	Name                      string
	Chunk                     [2]int32
	Continuous, Recount, Rain bool
	Biome                     string
	Initial                   [][4]int32
	Ticks                     []referenceRandomTick
}

func TestRandomTicksAgainstVanilla(t *testing.T) {
	b, err := os.ReadFile("../../testdata/worldgen/random_ticks_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []randomTickScene }
	if err = json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, scene := range fixture.Cases {
		t.Run(scene.Name, func(t *testing.T) { replayRandomTicks(t, scene) })
	}
}

func replayRandomTicks(t *testing.T, c randomTickScene) {
	dir := t.TempDir()
	// The first two ordinary oak loot rolls for this seed yield no items, as
	// in the reference chunk harness. Loot outcomes have a separate native trace.
	seed := int64(0)
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
	w.store.vanilla.flat.layers = nil
	cx, cz := c.Chunk[0], c.Chunk[1]
	load := func() {
		for x := cx - 1; x <= cx+1; x++ {
			for z := cz - 1; z <= cz+1; z++ {
				if _, err := w.LoadChunk(x, z); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	load()
	points := make([]BlockPosition, len(c.Initial))
	for i, v := range c.Initial {
		points[i] = BlockPosition{v[0], v[1], v[2]}
		if err := w.fallingEntityOperation(v[0]>>4, v[2]>>4, func(_ *Region, ch *Chunk) error {
			ch.SetBlock(int(v[0]&15), int(v[2]&15), v[1], uint32(v[3]))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if c.Rain {
		// The native section harness overrides isRaining to return true immediately.
		w.levels.Rain = 1
		id, err := registry.BiomeID("minecraft:" + c.Biome)
		if err != nil {
			t.Fatal(err)
		}
		for x := cx - 1; x <= cx+1; x++ {
			for z := cz - 1; z <= cz+1; z++ {
				if err := w.fallingEntityOperation(x, z, func(_ *Region, ch *Chunk) error {
					for i := range ch.biomes {
						for j := range ch.biomes[i] {
							ch.biomes[i][j] = id
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if c.Recount {
		if _, err := w.UnloadIdle(0); err != nil {
			t.Fatal(err)
		}
		load()
	}
	eligible := func(x, z int32) bool { return x == cx && z == cz }
	r, err := w.prepareScheduledLocked(eligible, false)
	if err != nil {
		t.Fatal(err)
	}
	var h *chunkScheduledTicks
	var dense []uint16
	if err := w.fallingEntityOperation(cx, cz, func(_ *Region, ch *Chunk) error { h = ch.scheduledTicks; dense = denseLightStates(ch); return nil }); err != nil {
		t.Fatal(err)
	}
	// The native harness supplies raw brightness. Import the same light
	// nibbles into a real light engine, so the crop uses its normal read path.
	lights := map[int32]*light.Engine{15: w.lighting}
	for _, v := range []int32{8, 9} {
		engine, err := light.New(MinY, WorldHeight, true)
		if err != nil {
			t.Fatal(err)
		}
		sky := make([][]byte, NumSections+2)
		nibble := byte(v * 17)
		for _, p := range points {
			sky[sectionOf(p.Y)+1] = bytes.Repeat([]byte{nibble}, 2048)
		}
		_, err = engine.Install(light.Position{X: cx, Z: cz}, dense, &light.Data{Sky: sky, Block: make([][]byte, len(sky)), Preserved: true})
		if err != nil {
			t.Fatal(err)
		}
		lights[v] = engine
	}
	defer func() { w.lighting = lights[15] }()
	for _, row := range c.Ticks {
		w.lighting = lights[row.Light]
		if !c.Continuous || row.Tick == 1 {
			w.randomPosition = uint32(row.LCGBefore)
			w.tickRandom = worldgen.NewLegacyRandom(row.Seed)
		}
		if int32(w.randomPosition) != row.LCGBefore {
			t.Fatalf("tick %d input position state", row.Tick)
		}
		w.advanceTimeLocked(1)
		for _, v := range row.Edits {
			state, err := BlockStateByID(uint32(v[3]))
			if err != nil {
				t.Fatal(err)
			}
			err = r.SetFluidBlock(worldgen.FeaturePosition{X: v[0], Y: v[1], Z: v[2]}, worldgen.SurfaceBlockState{Name: state.Name, Properties: state.Properties}, 3)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := r.tickScheduledQueues(eligible, true, false); err != nil {
			t.Fatal(err)
		}
		var samples, precip [][3]int32
		err := r.randomTickChunk(h, row.Speed, func(p worldgen.FeaturePosition, weather bool) {
			coord := [3]int32{p.X, p.Y, p.Z}
			if weather {
				precip = append(precip, coord)
			} else {
				samples = append(samples, coord)
			}
		})
		if err != nil {
			t.Fatalf("tick %d: %v", row.Tick, err)
		}
		if row.PendingFarmland != nil {
			got := w.scheduledTicks.blocks.Has(tickqueue.Position(points[0]), "minecraft:farmland")
			if got != *row.PendingFarmland {
				t.Fatalf("tick %d pending farmland %t, want %t", row.Tick, got, *row.PendingFarmland)
			}
		}
		states, err := w.ReadLoadedBlocks(points)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(states, row.States) || !slices.Equal(samples, row.Samples) || !slices.Equal(precip, row.Precipitation) || int32(w.randomPosition) != row.LCGAfter || w.tickRandom.State() != row.RNGAfter {
			t.Fatalf("tick %d states %v want %v; samples %v want %v; precipitation %v want %v; RNG %d/%d want %d/%d", row.Tick, states, row.States, samples, row.Samples, precip, row.Precipitation, int32(w.randomPosition), w.tickRandom.State(), row.LCGAfter, row.RNGAfter)
		}
	}
	if c.Name == "waterlogged_leaf_decay" {
		for _, p := range points {
			if !w.scheduledTicks.level.Has(tickqueue.Position(p), worldgen.FluidWater) {
				t.Fatalf("missing native water update at %v", p)
			}
		}
	}
}

func TestLeafLootSequencesAgainstVanilla(t *testing.T) {
	b, err := os.ReadFile("../../testdata/worldgen/leaf_decay_loot_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Seed   int64
		Rolls  int
		Tables map[string]map[string][]string
	}
	if err = json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seq := &lootSequences{dir: dir, seed: fixture.Seed}
	for i := 0; i < fixture.Rolls; i++ {
		if i == fixture.Rolls/2 {
			if err := seq.save(); err != nil {
				t.Fatal(err)
			}
			seq = &lootSequences{dir: dir, seed: fixture.Seed}
		}
		for name, expected := range fixture.Tables {
			drops, err := seq.leafDrops(name)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, drop := range drops {
				got = append(got, fmt.Sprintf("%s*%d", drop.item, drop.count))
			}
			if !slices.Equal(got, expected[strconv.Itoa(i)]) {
				t.Fatalf("%s roll %d: %v want %v", name, i, got, expected[strconv.Itoa(i)])
			}
		}
	}
}
