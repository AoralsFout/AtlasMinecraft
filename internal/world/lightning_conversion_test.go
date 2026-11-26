package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

type conversionState struct {
	Silent, Invulnerable, Burning, LeftHanded bool
	FollowBonus                               float64
	Type, Hand, Variant                       string
	Original, Baby, Persistent, NoAI, Named   bool
	Fire                                      int64
	Health                                    float32
	Sounds                                    int
}

func TestLightningConversionsAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/lightning_conversions_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Kind, Name string
		Ticks      []struct {
			conversionState
			SoundSeeds []int64
		}
	}
	if err := json.Unmarshal(raw, &scenes); err != nil || len(scenes) != 21 {
		t.Fatalf("vanilla captures %d: %v", len(scenes), err)
	}
	for _, s := range scenes {
		t.Run(s.Kind+"/"+s.Name, func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(2)
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
			if _, err := w.LoadChunk(-3, 2); err != nil {
				t.Fatal(err)
			}
			root, _ := w.store.vanilla.root.Object()
			level, _ := root.Object("Data")
			diff, ok := map[string]int8{"peaceful": 0, "easy": 1, "hard": 3}[s.Name]
			if !ok {
				diff = 2
			}
			level["Difficulty"] = byteTag(diff)
			w.weatherStopped = true
			if err := w.SetMobSpawning(false); err != nil {
				t.Fatal(err)
			}
			if err := w.SetFireTick(false); err != nil {
				t.Fatal(err)
			}
			id := [16]byte{9}
			pos := [3]float64{-40.5, 70, 40.5}
			rotation := nbt.Tag{Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: float32(37)}, {Type: nbt.Float, Value: float32(12)}}}
			kind := "minecraft:" + s.Kind
			d, _, err := defaultGeneratedEntity(kind)
			if err != nil || d == nil {
				t.Fatalf("defaults for %s: %v", kind, err)
			}
			d["id"] = nbt.Tag{Type: nbt.String, Value: kind}
			d["UUID"] = entityUUIDTag(id)
			d["Pos"] = entityPositionTag(pos)
			d["Rotation"] = rotation
			d["NoAI"] = byteTag(1)
			d["CustomNameVisible"] = byteTag(1)
			d["CustomName"] = nbt.Tag{Type: nbt.String, Value: `"Storm"`}
			d["Age"] = nbt.Tag{Type: nbt.Int, Value: int32(-100)}
			if s.Name == "adult" {
				delete(d, "NoAI")
				delete(d, "CustomName")
				delete(d, "CustomNameVisible")
				d["Age"] = nbt.Tag{Type: nbt.Int, Value: int32(0)}
				d["Silent"] = byteTag(1)
				d["Invulnerable"] = byteTag(1)
			}
			d["PersistenceRequired"] = byteTag(0)
			d["Fire"] = nbt.Tag{Type: nbt.Short, Value: int16(-1)}
			d["Health"] = nbt.Tag{Type: nbt.Float, Value: float32(19)}
			if s.Kind == "mooshroom" && s.Name == "hard" {
				d["Type"] = nbt.Tag{Type: nbt.String, Value: "brown"}
			}
			actor, err := worldEntityFromNBT(d)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.fallingEntityOperation(-3, 2, func(_ *Region, c *Chunk) error { c.entities = []WorldEntity{actor}; c.indexLivingDamage(); return nil }); err != nil {
				t.Fatal(err)
			}
			w.tickRandom = worldgen.NewLegacyRandom(95)
			if s.Name == "adult" {
				w.tickRandom = worldgen.NewLegacyRandom(1)
			}
			w.soundRandom = worldgen.NewLegacyRandom(19)
			var last WorldEntity
			for i, row := range s.Ticks {
				want := row.conversionState
				var seeds []int64
				if i == 0 || s.Name == "two_bolts" && i == 3 {
					boltSeed := int64(6)
					if i == 3 {
						boltSeed = 9
					}
					w.addLightning(WorldEntity{Type: LightningEntity, UUID: [16]byte{byte(i + 1)}, Position: pos}, s.Name == "visual", boltSeed)
				}
				sounds := 0
				var order []string
				err := w.Tick(TickChunks{Entities: func(int32, int32) bool { return true }, livingChanged: func(c LivingDamageChange) {
					switch {
					case c.Spawned:
						order = append(order, "add")
					case c.Removed:
						order = append(order, "remove")
					case c.MooshroomVariant != "":
						if silent, _ := c.Entity.Data.Number("Silent"); silent == 0 {
							sounds++
							seeds = append(seeds, c.SoundSeed)
						}
					}
				}})
				if err != nil {
					t.Fatal(err)
				}
				if i == 0 && !want.Original && !slices.Equal(order, []string{"add", "remove"}) {
					t.Fatalf("replacement events %v", order)
				}
				actors, err := w.EntitiesInChunk(-3, 2)
				if err != nil || len(actors) != 1 {
					t.Fatalf("tick %d: %d actors: %v", i+1, len(actors), err)
				}
				e := actors[0]
				last = e
				got := conversionState{Type: e.Type, Hand: "minecraft:air", Original: e.UUID == id, Sounds: sounds}
				got.Fire, _ = e.Data.Number("Fire")
				got.Health, _ = e.Data["Health"].Value.(float32)
				baby, _ := e.Data.Number("IsBaby")
				age, _ := e.Data.Number("Age")
				got.Baby = baby != 0 || age < 0
				persistent, _ := e.Data.Number("PersistenceRequired")
				got.Persistent = persistent != 0
				noAI, _ := e.Data.Number("NoAI")
				got.NoAI = noAI != 0
				name, _ := e.Data.Text("CustomName")
				visible, _ := e.Data.Number("CustomNameVisible")
				got.Named = name == `"Storm"` && visible != 0
				silent, _ := e.Data.Number("Silent")
				got.Silent = silent != 0
				inv, _ := e.Data.Number("Invulnerable")
				got.Invulnerable = inv != 0
				got.Burning = e.OnFire
				if e.Type == "minecraft:witch" {
					left, _ := e.Data.Number("LeftHanded")
					got.LeftHanded = left != 0
					attrs, _ := e.Data["attributes"].Elements()
					for _, tag := range attrs {
						a, _ := tag.Object()
						mods, _ := a["modifiers"].Elements()
						for _, tag := range mods {
							m, _ := tag.Object()
							if id, _ := m.Text("id"); id == "minecraft:random_spawn_bonus" {
								got.FollowBonus, _ = m["amount"].Value.(float64)
							}
						}
					}
				}
				if e.Type == "minecraft:mooshroom" {
					got.Variant, _ = e.Data.Text("Type")
					if got.Variant == "" {
						got.Variant = "red"
					}
				}
				hands, _ := e.Data["HandItems"].Elements()
				if len(hands) > 0 {
					stack, _ := hands[0].Object()
					if item, ok := stack.Text("id"); ok {
						got.Hand = item
					}
				}
				if !slices.Equal(row.SoundSeeds, seeds) {
					t.Fatalf("tick %d sound seeds: %v, want %v", i+1, seeds, row.SoundSeeds)
				}
				if got != want || e.Position != pos || e.Rotation != [2]float32{37, 12} {
					t.Fatalf("tick %d: %+v, want %+v; position/rotation %v %v", i+1, got, want, e.Position, e.Rotation)
				}
			}
			if s.Name == "normal" {
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				w, err = Open(noViewers{}, 1, dir)
				if err != nil {
					t.Fatal(err)
				}
				actors, err := w.EntitiesInChunk(-3, 2)
				if err != nil || len(actors) != 1 || actors[0].UUID != last.UUID || actors[0].Type != last.Type {
					t.Fatalf("reloaded replacement: %v (%v)", actors, err)
				}
				if s.Kind == "mooshroom" {
					if variant, _ := actors[0].Data.Text("Type"); variant != "brown" {
						t.Fatal("lost saved mooshroom color")
					}
					// Reusing the original bolt UUID distinguishes runtime memory
					// from a UUID incorrectly written into the mooshroom's save.
					w.addLightning(WorldEntity{Type: LightningEntity, UUID: [16]byte{1}, Position: pos}, false, 6)
					if err := w.Tick(TickChunks{Entities: func(int32, int32) bool { return true }}); err != nil {
						t.Fatal(err)
					}
					actors, err = w.EntitiesInChunk(-3, 2)
					if err != nil || len(actors) != 1 {
						t.Fatal("reloaded mooshroom", err)
					}
					if variant, _ := actors[0].Data.Text("Type"); variant != "red" {
						t.Fatal("retained last bolt across reload")
					}
				}
			}
		})
	}
}
