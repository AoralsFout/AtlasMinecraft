package world

import (
	"atlas/internal/nbt"
	"encoding/json"
	"os"
	"testing"
)

func TestCreeperFuseAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/creeper_fuse_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Name                   string
		Seed                   int64
		Charged, Ignited, NoAI bool
		Ticks                  []struct {
			Distance                        float64
			Visible, Primed, DirectionDirty bool
			Direction, Swell                int32
			Power                           *float32
		}
	}
	if err := json.Unmarshal(raw, &scenes); err != nil || len(scenes) != 8 {
		t.Fatalf("native scenes %d: %v", len(scenes), err)
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
			t.Cleanup(func() { w.Close() })
			if _, err := w.LoadChunk(0, 0); err != nil {
				t.Fatal(err)
			}
			pos := [3]float64{.5, 70, .5}
			uuid := [16]byte{7}
			data := nbt.Object{
				"id": {Type: nbt.String, Value: "minecraft:creeper"}, "UUID": entityUUIDTag(uuid), "Pos": entityPositionTag(pos),
				"Rotation": {Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: float32(0)}, {Type: nbt.Float, Value: float32(0)}}},
				"Health":   {Type: nbt.Float, Value: float32(20)},
			}
			if scene.Charged {
				data["powered"] = byteTag(1)
			}
			if scene.Ignited {
				data["ignited"] = byteTag(1)
			}
			if scene.NoAI {
				data["NoAI"] = byteTag(1)
			}
			if scene.Name == "custom" {
				data["Fuse"] = nbt.Tag{Type: nbt.Short, Value: int16(4)}
				data["ExplosionRadius"] = byteTag(5)
			}
			fuse := newCreeperFuse(scene.Seed)
			mob := WorldEntity{UUID: uuid, Data: data, Type: "minecraft:creeper", RuntimeID: 77, Position: pos, fuse: &fuse}
			if err := w.fallingEntityOperation(0, 0, func(_ *Region, ch *Chunk) error {
				ch.entities = []WorldEntity{mob}
				ch.indexLivingDamage()
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			wall := false
			for i, row := range scene.Ticks {
				w.simulationPlayers = []SimulationPlayer{{X: pos[0] + row.Distance, Y: 70, Z: .5, ID: 1}}
				if wall == row.Visible {
					var state uint32
					if !row.Visible {
						state = 1
					}
					if err := w.SetBlock(1, 71, 0, state); err != nil {
						t.Fatal(err)
					}
					wall = !row.Visible
				}
				var event CreeperFuseEvent
				var count int
				if err := w.Tick(TickChunks{Entities: func(int32, int32) bool { return true }, livingChanged: func(c LivingDamageChange) {
					if c.Fuse != nil {
						event = *c.Fuse
						mob = c.Entity
						count++
					}
				}}); err != nil {
					t.Fatal(err)
				}
				entities, err := w.EntitiesInChunk(0, 0)
				if err != nil {
					t.Fatal(err)
				}
				if row.Power != nil {
					if len(entities) != 0 || !event.Exploded || event.Power != *row.Power {
						t.Fatalf("tick %d: explosion %+v", i+1, event)
					}
				} else if len(entities) == 1 {
					mob = entities[0]
				} else {
					t.Fatalf("tick %d: lost creeper", i+1)
				}
				if count > 1 || event.Primed != row.Primed || event.DirectionChanged != (row.DirectionDirty && row.Power == nil) || mob.fuse.swell != row.Swell || mob.CreeperSwellDirection() != row.Direction {
					t.Fatalf("tick %d: dir %d, swell %d, event %+v; native %+v", i+1, mob.CreeperSwellDirection(), mob.fuse.swell, event, row)
				}
			}
		})
	}
}

func TestCreeperTargetsSurvivalButIgnoresCreativeAndSpectator(t *testing.T) {
	e := WorldEntity{Type: "minecraft:creeper", RuntimeID: 77}
	state := newCreeperFuse(0)
	players := []SimulationPlayer{{ID: 1, X: 1, Creative: true}, {ID: 2, X: 1, Spectator: true}, {ID: 3, X: 2}}
	see := func(SimulationPlayer) (bool, error) { return true, nil }
	for tick := 0; tick < 32; tick++ {
		if _, err := state.tick(e, players[:2], false, see); err != nil {
			t.Fatal(err)
		}
		if state.direction != -1 || state.swell != 0 {
			t.Fatal("creative or spectator primed creeper")
		}
	}
	for tick := 0; tick < 80 && state.direction < 0; tick++ {
		if _, err := state.tick(e, players, false, see); err != nil {
			t.Fatal(err)
		}
	}
	if state.target != 3 || state.direction != 1 {
		t.Fatal("survival player was not acquired", state)
	}
}
