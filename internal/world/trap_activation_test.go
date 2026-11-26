package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"encoding/json"
	"os"
	"testing"
)

func TestSkeletonTrapActivationAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/protocol/skeleton_trap_activation_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Name        string
		ID, Initial int32
		Ticks       []struct {
			Offset           [3]float64
			Spectator, Armed bool
			Timer            *int64
			Spawns           []int32
		}
	}
	if err := json.Unmarshal(raw, &scenes); err != nil || len(scenes) != 5 {
		t.Fatal("vanilla capture", len(scenes), err)
	}
	for _, scene := range scenes {
		t.Run(scene.Name, func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(42)
			if err := CreateFlatWorld(dir, &seed); err != nil {
				t.Fatal(err)
			}
			open := func() *World {
				t.Helper()
				w, err := Open(noViewers{}, 1, dir)
				if err != nil {
					t.Fatal(err)
				}
				return w
			}
			w := open()
			defer func() {
				if err := w.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := w.SetMobSpawning(false); err != nil {
				t.Fatal(err)
			}
			if _, err := w.LoadChunk(0, 0); err != nil {
				t.Fatal(err)
			}
			r := fluidRuntime{world: w}
			horse, err := r.spawnSkeletonTrap(worldgen.FeaturePosition{X: 8, Y: -59, Z: 8})
			if err != nil {
				t.Fatal(err)
			}
			if err := w.fallingEntityOperation(0, 0, func(_ *Region, ch *Chunk) error {
				ch.entities[0].RuntimeID = scene.ID // the captured horse's goal-selector phase
				ch.entities[0].Data["SkeletonTrapTime"] = nbt.Tag{Type: nbt.Int, Value: scene.Initial}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			entities := func() []WorldEntity {
				t.Helper()
				list, err := w.EntitiesInChunk(0, 0)
				if err != nil {
					t.Fatal(err)
				}
				return list
			}
			var changes []TrapChange
			chunks := TickChunks{Entities: func(x, z int32) bool { return x == 0 && z == 0 }, trapChanged: func(c TrapChange) { changes = append(changes, c) }}
			for i, row := range scene.Ticks {
				changes = nil
				w.simulationPlayers = []SimulationPlayer{{X: 8 + row.Offset[0], Y: -59 + row.Offset[1], Z: 8 + row.Offset[2], Spectator: row.Spectator}}
				if err := w.Tick(chunks); err != nil {
					t.Fatal(err)
				}
				list := entities()
				if row.Timer == nil {
					if len(list) != 0 || len(changes) != 1 || changes[0].Original.UUID != horse.UUID || len(changes[0].Mounts) != 0 {
						t.Fatalf("tick %d: trap did not expire alone", i+1)
					}
					continue
				}
				if len(list) == 0 || list[0].UUID != horse.UUID {
					t.Fatalf("tick %d: lost original horse", i+1)
				}
				timer, ok := list[0].Data.Number("SkeletonTrapTime")
				if !ok || timer != *row.Timer || armedTrap(list[0]) != row.Armed {
					t.Fatalf("tick %d: timer %d armed %v, want %d/%v", i+1, timer, armedTrap(list[0]), *row.Timer, row.Armed)
				}
				if len(row.Spawns) == 0 {
					if len(list) != 1 || len(changes) != 0 {
						t.Fatalf("unexpected activation on tick %d", i+1)
					}
				} else if len(changes) != 1 || changes[0].Bolt.Position != horse.Position || len(w.bolts) != 1 || !w.bolts[0].visualOnly || len(list) != 4 {
					t.Fatalf("tick %d: incorrect lightning or horse group", i+1)
				}
				// Persist an armed timer across shutdown and a tick with entity processing disabled.
				if scene.Name == "expire" && i == 0 {
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
					w = open()
					if _, err := w.LoadChunk(0, 0); err != nil {
						t.Fatal(err)
					}
					if err := w.Tick(TickChunks{}); err != nil {
						t.Fatal(err)
					}
					if list := entities(); len(list) != 1 || list[0].Data["SkeletonTrapTime"].Value != int32(*row.Timer) {
						t.Fatal("paused or reloaded trap lost its timer")
					}
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			w = open()
			list := entities()
			last := scene.Ticks[len(scene.Ticks)-1]
			if last.Timer == nil {
				if len(list) != 0 {
					t.Fatal("expired trap reappeared")
				}
				return
			}
			if len(list) != 4 || list[0].UUID != horse.UUID {
				t.Fatal("saved horse group", len(list))
			}
			seen := make(map[[16]byte]bool)
			for _, mount := range list {
				riders, err := EntityPassengers(mount)
				tame, _ := mount.Data.Number("Tame")
				if err != nil || tame != 1 || armedTrap(mount) || len(riders) != 1 || riders[0].Type != "minecraft:skeleton" {
					t.Fatal("invalid saved mount", mount.UUID, err)
				}
				hands, _ := riders[0].Data["HandItems"].Elements()
				armor, _ := riders[0].Data["ArmorItems"].Elements()
				if len(hands) != 2 || len(armor) != 4 {
					t.Fatal("missing saved rider equipment")
				}
				hand, _ := hands[0].Object()
				head, _ := armor[3].Object()
				if bow, _ := hand.Text("id"); bow != "minecraft:bow" || len(head) == 0 {
					t.Fatal("rider lost bow or helmet", hand, head)
				}
				for _, actor := range []WorldEntity{mount, riders[0]} {
					if seen[actor.UUID] {
						t.Fatal("duplicate actor UUID", actor.UUID)
					}
					seen[actor.UUID] = true
				}
			}
			changes = nil
			w.simulationPlayers = []SimulationPlayer{{X: 8, Y: -59, Z: 8}}
			if err := w.Tick(chunks); err != nil || len(changes) != 0 {
				t.Fatal("activated horse triggered again", err)
			}
		})
	}
}
