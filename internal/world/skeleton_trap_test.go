package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
)

func TestSkeletonTrapTicksAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/skeleton_traps_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Base                         int8
		InitialDay, InitialInhabited int64
		Rod, MobSpawning             bool
		Ticks                        []struct {
			Seed                int64
			RNGAfter            uint64
			LCGBefore, LCGAfter int32
			Inhabited           int64
			Difficulty, Health  float32
			Added               []string
			Trap, VisualOnly    bool
			HorsePos            [3]float64
			Age, TrapTime       int64
		}
	}
	if err := json.Unmarshal(raw, &scenes); err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 11 {
		t.Fatalf("got %d vanilla trajectories", len(scenes))
	}
	const cx, cz = -3, 2
	target := worldgen.FeaturePosition{X: -40, Y: 70, Z: 40}
	center := func(x, z int32) bool { return x == cx && z == cz }
	for index, scene := range scenes {
		t.Run(fmt.Sprintf("%02d_base%d_day%d_inhabited%d_rod%t_mobs%t", index, scene.Base, scene.InitialDay, scene.InitialInhabited, scene.Rod, scene.MobSpawning), func(t *testing.T) {
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
			w.store.vanilla.flat.layers = nil
			for z := int32(cz - 1); z <= cz+1; z++ {
				for x := int32(cx - 1); x <= cx+1; x++ {
					if _, err := w.LoadChunk(x, z); err != nil {
						t.Fatal(err)
					}
				}
			}
			block := "stone"
			if scene.Rod {
				block = "lightning_rod"
			}
			id, err := BlockStateID(block, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.SetBlock(target.X, target.Y-1, target.Z, id); err != nil {
				t.Fatal(err)
			}
			root, _ := w.store.vanilla.root.Object()
			data, _ := root.Object("Data")
			data["Difficulty"] = nbt.Tag{Type: nbt.Byte, Value: scene.Base}
			if err := w.SetMobSpawning(scene.MobSpawning); err != nil {
				t.Fatal(err)
			}
			if err := w.store.vanilla.saveGameRule("randomTickSpeed", "0", nil); err != nil {
				t.Fatal(err)
			}
			w.SetDayTime(scene.InitialDay)
			w.meta.Raining, w.meta.Thundering = true, true
			w.weatherStopped = true
			w.levels = WeatherLevels{Rain: 1, Thunder: 1}
			var holder *chunkScheduledTicks
			if err := w.fallingEntityOperation(cx, cz, func(_ *Region, c *Chunk) error { holder = c.scheduledTicks; return nil }); err != nil {
				t.Fatal(err)
			}
			holder.inhabitedTime = scene.InitialInhabited
			var horseID [16]byte
			var wantHorses int
			for i, row := range scene.Ticks {
				if i <= 1 {
					w.tickRandom = worldgen.NewLegacyRandom(row.Seed)
					w.randomPosition = uint32(row.LCGBefore)
				}
				var added []string
				var lastHorse *WorldEntity
				chunks := TickChunks{Blocks: center, Random: center, Entities: center, lightningAdded: func(bolt WorldEntity, horse *WorldEntity) {
					if horse != nil {
						added = append(added, horse.Type)
						lastHorse = horse
					}
					added = append(added, bolt.Type)
				}}
				if err := w.Tick(chunks); err != nil {
					t.Fatal(err)
				}
				if got := w.localDifficulty(target); got != row.Difficulty || holder.inhabitedTime != row.Inhabited {
					t.Fatalf("tick %d difficulty %v inhabited %d, want %v and %d", i+1, got, holder.inhabitedTime, row.Difficulty, row.Inhabited)
				}
				if !slices.Equal(added, row.Added) || int32(w.randomPosition) != row.LCGAfter || w.tickRandom.State() != row.RNGAfter {
					t.Fatalf("tick %d added %v LCG %d RNG %d, want %v %d %d", i+1, added, int32(w.randomPosition), w.tickRandom.State(), row.Added, row.LCGAfter, row.RNGAfter)
				}
				if len(added) > 0 && (len(w.bolts) != 1 || w.bolts[0].visualOnly != row.VisualOnly) {
					t.Fatalf("tick %d visualOnly want %t", i+1, row.VisualOnly)
				}
				if lastHorse != nil {
					wantHorses++
					horseID = lastHorse.UUID
					age, _ := lastHorse.Data.Number("Age")
					trap, _ := lastHorse.Data.Number("SkeletonTrap")
					timer, _ := lastHorse.Data.Number("SkeletonTrapTime")
					if lastHorse.Position != row.HorsePos || (trap != 0) != row.Trap || timer != row.TrapTime || age != row.Age || lastHorse.Data["Health"].Value != row.Health {
						t.Fatalf("tick %d incorrect horse %v", i+1, lastHorse)
					}
				}
			}
			if scene.InitialInhabited > 0 {
				neighbor := target
				neighbor.X += 16
				if got := w.localDifficulty(neighbor); got >= w.localDifficulty(target) {
					t.Fatalf("uninhabited neighbor inherited strike chunk difficulty %v", got)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(noViewers{}, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			w = reopened
			entities, err := w.EntitiesInChunk(cx, cz)
			if err != nil {
				t.Fatal(err)
			}
			if len(entities) != wantHorses || wantHorses == 1 && (entities[0].UUID != horseID || entities[0].Type != SkeletonHorseEntity) {
				t.Fatalf("reloaded entities %v", entities)
			}
			for _, e := range entities {
				trap, ok := e.Data.Number("SkeletonTrap")
				if !ok || trap != 1 || e.Position != [3]float64{-40, 70, 40} {
					t.Fatalf("reloaded trap %v at %v", trap, e.Position)
				}
			}
			err = w.fallingEntityOperation(cx, cz, func(_ *Region, c *Chunk) error {
				if c.scheduledTicks.inhabitedTime != scene.Ticks[len(scene.Ticks)-1].Inhabited {
					return fmt.Errorf("reloaded InhabitedTime %d", c.scheduledTicks.inhabitedTime)
				}
				return nil
			})
			if err != nil || w.MobSpawning() != scene.MobSpawning {
				t.Fatalf("reloaded rule %t, error %v", w.MobSpawning(), err)
			}
		})
	}
}
