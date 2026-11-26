package world

import (
	"atlas/internal/nbt"
	"atlas/internal/proto"
	"atlas/internal/worldgen"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

type copperTick struct {
	OriginReads, Probes, Writes, Events [][4]int32
	WorldRNG, BoltRNG                   uint64
	Removed                             bool
}

func TestNaturalLightningCleansCopperBehindRod(t *testing.T) {
	dir := t.TempDir()
	seed := int64(95)
	if err := CreateFlatWorld(dir, &seed); err != nil {
		t.Fatal(err)
	}
	w, err := Open(noViewers{}, 1, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.LoadChunk(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.SetFireTick(false); err != nil {
		t.Fatal(err)
	}
	if err := w.SetMobSpawning(false); err != nil {
		t.Fatal(err)
	}
	w.weatherStopped = true
	w.levels = WeatherLevels{Rain: 1, Thunder: 1}
	w.tickRandom = worldgen.NewLegacyRandom(seed)
	slab := map[string]string{"type": "top"}
	corroded, err := BlockStateID("oxidized_cut_copper_slab", slab)
	if err != nil {
		t.Fatal(err)
	}
	clean, err := BlockStateID("cut_copper_slab", slab)
	if err != nil {
		t.Fatal(err)
	}
	rod, err := BlockStateID("lightning_rod", map[string]string{"facing": "east"})
	if err != nil {
		t.Fatal(err)
	}
	for x, id := range map[int32]uint32{4: corroded, 5: rod, 10: corroded} {
		if err := w.SetBlock(x, 69, 5, id); err != nil {
			t.Fatal(err)
		}
	}
	r := fluidRuntime{world: w, settings: w.fluidSettings()}
	if err := r.naturalLightning(worldgen.FeaturePosition{X: 10, Z: 5}); err != nil {
		t.Fatal(err)
	}
	if len(w.bolts) != 1 || w.bolts[0].entity.Position != [3]float64{5.5, 70, 5.5} {
		t.Fatalf("unexpected weather strike: %v", w.LightningEntities())
	}
	if err := w.Tick(TickChunks{Entities: func(x, z int32) bool { return x == 0 && z == 0 }}); err != nil {
		t.Fatal(err)
	}
	for x, want := range map[int32]uint32{4: clean, 10: corroded} {
		if id, err := w.BlockAt(x, 69, 5); err != nil || id != want {
			t.Fatalf("slab at x=%d: %d, want %d, err %v", x, id, want, err)
		}
	}
}

func checkCopperSearch(t *testing.T, seed int64, tick copperTick) {
	t.Helper()
	if len(tick.OriginReads) == 0 {
		t.Fatal("native first tick has no strike position")
	}
	last := tick.OriginReads[len(tick.OriginReads)-1]
	origin := worldgen.FeaturePosition{X: last[0], Y: last[1], Z: last[2]}
	state, err := BlockStateByID(uint32(last[3]))
	if err != nil {
		t.Fatal(err)
	}
	rng := worldgen.NewLegacyRandom(seed)
	i := 0
	if _, ok := copperWeathering[state.Name]; ok {
		err := walkLightningCopper(origin, rng, func(p worldgen.FeaturePosition) (bool, error) {
			if i >= len(tick.Probes) {
				t.Fatalf("unexpected candidate %d: %v", i, p)
			}
			c := tick.Probes[i]
			if p.X != c[0] || p.Y != c[1] || p.Z != c[2] {
				t.Fatalf("candidate %d: %v, want %v", i, p, c)
			}
			i++
			sampled, err := BlockStateByID(uint32(c[3]))
			_, eligible := copperWeathering[sampled.Name]
			return eligible, err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if i != len(tick.Probes) || rng.State() != tick.WorldRNG {
		t.Fatalf("search ended at candidate %d/%d, RNG %d/%d", i, len(tick.Probes), rng.State(), tick.WorldRNG)
	}
}

func copperPackets(t *testing.T, sink *fluidEffectSink) (events, writes [][4]int32) {
	t.Helper()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, p := range sink.packets {
		if p.id != proto.SLevelEvent && p.id != proto.SBlockChange {
			continue
		}
		r := proto.NewReader(p.data)
		if p.id == proto.SLevelEvent {
			kind, err := r.I32()
			if err != nil {
				t.Fatal(err)
			}
			if kind != 3002 {
				continue
			}
		}
		x, y, z, err := r.Position()
		if err != nil {
			t.Fatal(err)
		}
		if p.id == proto.SBlockChange {
			state, err := r.VarInt()
			if err != nil {
				t.Fatal(err)
			}
			writes = append(writes, [4]int32{x, y, z, state})
			continue
		}
		data, err := r.I32()
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, [4]int32{x, y, z, data})
	}
	sink.packets = nil
	return events, writes
}

func TestLightningCopperAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/lightning_copper_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Name                        string
		Chunk                       [2]int32
		Initial                     [][4]int32
		Seed, WorldSeed, SearchSeed int64
		Difficulty                  int8
		Visual, Fire                bool
		Position                    [3]float64
		Ticks                       []copperTick
	}
	if err := json.Unmarshal(raw, &scenes); err != nil || len(scenes) != 16 {
		t.Fatalf("copper reference: %d scenes, %v", len(scenes), err)
	}
	for _, scene := range scenes {
		t.Run(scene.Name, func(t *testing.T) {
			checkCopperSearch(t, scene.SearchSeed, scene.Ticks[0])
			dir := t.TempDir()
			seed := int64(1)
			if err := CreateFlatWorld(dir, &seed); err != nil {
				t.Fatal(err)
			}
			sink := &fluidEffectSink{}
			w, err := Open(sink, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := w.Close(); err != nil {
					t.Error(err)
				}
			}()
			w.weatherStopped = true
			w.store.vanilla.flat.layers = nil
			root, _ := w.store.vanilla.root.Object()
			data, _ := root.Object("Data")
			data["Difficulty"] = nbt.Tag{Type: nbt.Byte, Value: scene.Difficulty}
			if err := w.SetFireTick(scene.Fire); err != nil {
				t.Fatal(err)
			}
			cx, cz := scene.Chunk[0], scene.Chunk[1]
			for x := cx - 1; x <= cx+1; x++ {
				for z := cz - 1; z <= cz+1; z++ {
					if _, err := w.LoadChunk(x, z); err != nil {
						t.Fatal(err)
					}
				}
			}
			want := map[BlockPosition]uint32{}
			for _, c := range scene.Initial {
				want[BlockPosition{c[0], c[1], c[2]}] = uint32(c[3])
				if _, err := w.editBlock(setBlockMsg{x: c[0], y: c[1], z: c[2], state: uint32(c[3]), raw: true, deferSave: true}); err != nil {
					t.Fatal(err)
				}
			}
			center := func(x, z int32) bool { return x == cx && z == cz }
			w.tickRandom = worldgen.NewLegacyRandom(scene.WorldSeed)
			w.addLightning(WorldEntity{Type: LightningEntity, Position: scene.Position}, scene.Visual, scene.Seed)
			boltRNG := w.bolts[0].random
			for i, row := range scene.Ticks {
				if err := w.Tick(TickChunks{Blocks: center, Entities: center}); err != nil {
					t.Fatalf("tick %d: %v", i+1, err)
				}
				if (len(w.bolts) == 0) != row.Removed || w.tickRandom.State() != row.WorldRNG || boltRNG.State() != row.BoltRNG {
					t.Fatalf("tick %d: removed=%v world/bolt RNG=%d/%d, want %v %d/%d", i+1, len(w.bolts) == 0, w.tickRandom.State(), boltRNG.State(), row.Removed, row.WorldRNG, row.BoltRNG)
				}
				events, writes := copperPackets(t, sink)
				if !slices.Equal(events, row.Events) {
					t.Fatalf("tick %d: sparks %v, want %v", i+1, events, row.Events)
				}
				if !slices.Equal(writes, row.Writes) {
					t.Fatalf("tick %d: writes %v, want %v", i+1, writes, row.Writes)
				}
				for _, c := range row.Writes {
					want[BlockPosition{c[0], c[1], c[2]}] = uint32(c[3])
				}
				var positions []BlockPosition
				for p := range want {
					positions = append(positions, p)
				}
				states, err := w.ReadLoadedBlocks(positions)
				if err != nil {
					t.Fatal(err)
				}
				for j, p := range positions {
					if states[j] != want[p] {
						t.Fatalf("tick %d: %v state %d, want %d", i+1, p, states[j], want[p])
					}
				}
			}
		})
	}
}
