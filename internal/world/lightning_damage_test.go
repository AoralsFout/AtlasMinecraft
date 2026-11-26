package world

import (
	"atlas/internal/nbt"
	"encoding/json"
	"math"
	"os"
	"slices"
	"testing"
)

func TestLightningLivingHitsAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/lightning_hits_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Name      string
		Visual    bool
		Seed      int64
		Position  [3]float64
		Positions [][3]float64
		Ticks     []struct {
			Struck []int
			Mobs   [][6]float64
		}
	}
	if err := json.Unmarshal(raw, &scenes); err != nil || len(scenes) != 8 {
		t.Fatalf("native living captures: %d, %v", len(scenes), err)
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
			defer w.Close()
			w.weatherStopped = true
			if scene.Name == "rain" {
				w.meta.Raining = true
				w.levels.Rain = 1
			}
			if err := w.SetFireTick(false); err != nil {
				t.Fatal(err)
			}
			keys := []chunkKey{{-3, 2}, {-2, 2}}
			for _, k := range keys {
				if _, err := w.LoadChunk(k.x, k.z); err != nil {
					t.Fatal(err)
				}
			}
			for i, p := range scene.Positions {
				fire := int16(-1)
				if scene.Name == "loaded_zero" {
					fire = 0
				}
				id := [16]byte{byte(i + 1)}
				data, _, err := defaultGeneratedEntity("minecraft:cow")
				if err != nil || data == nil {
					t.Fatalf("cow defaults: %v", err)
				}
				data["id"] = nbt.Tag{Type: nbt.String, Value: "minecraft:cow"}
				data["UUID"] = entityUUIDTag(id)
				data["Pos"] = entityPositionTag(p)
				data["Health"] = nbt.Tag{Type: nbt.Float, Value: float32(40)}
				data["Fire"] = nbt.Tag{Type: nbt.Short, Value: fire}
				if scene.Name == "armor" {
					var attrs []nbt.Tag
					for key, v := range map[string]float64{"armor": 12, "armor_toughness": 2} {
						a := nbt.Object{"id": {Type: nbt.String, Value: "minecraft:generic." + key}, "base": {Type: nbt.Double, Value: v}}
						attrs = append(attrs, nbt.Tag{Type: nbt.Compound, Value: a})
					}
					data["attributes"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: attrs}
					data["AbsorptionAmount"] = nbt.Tag{Type: nbt.Float, Value: float32(2)}
				}
				if scene.Name == "invulnerable" {
					data["Invulnerable"] = byteTag(1)
				}
				if scene.Name == "equipped" {
					var items []nbt.Tag
					for _, id := range []string{"", "", "minecraft:netherite_chestplate", "minecraft:iron_helmet"} {
						stack := nbt.Object{}
						if id != "" {
							stack["id"] = nbt.Tag{Type: nbt.String, Value: id}
							stack["count"] = nbt.Tag{Type: nbt.Int, Value: int32(1)}
						}
						items = append(items, nbt.Tag{Type: nbt.Compound, Value: stack})
					}
					data["ArmorItems"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: items}
				}
				e, err := worldEntityFromNBT(data)
				if err != nil {
					t.Fatal(err)
				}
				k := chunkKey{int32(math.Floor(p[0])) >> 4, int32(math.Floor(p[2])) >> 4}
				if err := w.fallingEntityOperation(k.x, k.z, func(_ *Region, ch *Chunk) error {
					ch.entities = append(ch.entities, e)
					ch.indexLivingDamage()
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			w.addLightning(WorldEntity{Type: LightningEntity, Position: scene.Position}, scene.Visual, scene.Seed)
			for i, row := range scene.Ticks {
				if scene.Name == "move" && i == 1 {
					if err := w.fallingEntityOperation(-3, 2, func(_ *Region, ch *Chunk) error {
						for j := range ch.entities {
							e := &ch.entities[j]
							switch e.UUID[0] {
							case 1:
								e.Position[2] += 6
							case 10:
								e.Position = scene.Position
							default:
								continue
							}
							e.Data["Pos"] = entityPositionTag(e.Position)
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				if err := w.Tick(TickChunks{Entities: func(int32, int32) bool { return true }}); err != nil {
					t.Fatal(err)
				}
				var hit []int
				for _, k := range keys {
					err := w.fallingEntityOperation(k.x, k.z, func(_ *Region, ch *Chunk) error {
						for _, e := range ch.entities {
							idx := int(e.UUID[0]) - 1
							hp := e.Data["Health"].Value.(float32)
							fire, _ := e.Data.Number("Fire")
							hurt, _ := e.Data.Number("HurtTime")
							absorption, _ := e.Data["AbsorptionAmount"].Value.(float32)
							actual := [6]float64{float64(hp), float64(fire), float64(hurt), float64(ch.hurtCooldowns[e.UUID].ticks), float64(absorption), 0}
							if e.OnFire {
								actual[5] = 1
							}
							if actual != row.Mobs[idx] {
								t.Errorf("tick %d mob %d: health/fire/hurt/cooldown %v, want %v", i+1, idx, actual, row.Mobs[idx])
							}
							if len(w.bolts) > 0 && !scene.Visual && w.bolts[0].life >= 0 && lightningIntersects(scene.Position, e) {
								hit = append(hit, idx)
							}
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				slices.Sort(hit)
				if !slices.Equal(hit, row.Struck) {
					t.Fatalf("tick %d hit %v, native %v", i+1, hit, row.Struck)
				}
				if t.Failed() {
					t.FailNow()
				}
			}
		})
	}
}

func TestLightningDamageReloadAndDismount(t *testing.T) {
	dir := t.TempDir()
	seed := int64(5)
	if err := CreateFlatWorld(dir, &seed); err != nil {
		t.Fatal(err)
	}
	open := func() *World {
		w, err := Open(noViewers{}, 1, dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.LoadChunk(0, 0); err != nil {
			t.Fatal(err)
		}
		w.weatherStopped = true
		return w
	}
	w := open()
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := w.SetFireTick(false); err != nil {
		t.Fatal(err)
	}
	pos := [3]float64{8.5, 70, 8.5}
	cow := func(id byte, health float32) WorldEntity {
		d, _, err := defaultGeneratedEntity("minecraft:cow")
		if err != nil {
			t.Fatal(err)
		}
		d["id"] = nbt.Tag{Type: nbt.String, Value: "minecraft:cow"}
		d["UUID"] = entityUUIDTag([16]byte{id})
		d["Pos"] = entityPositionTag(pos)
		d["Health"] = nbt.Tag{Type: nbt.Float, Value: health}
		d["Fire"] = nbt.Tag{Type: nbt.Short, Value: int16(-1)}
		e, err := worldEntityFromNBT(d)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	survivor := cow(1, 15)
	mount := cow(2, 3)
	rider := cow(3, 15)
	mount.Data["Passengers"] = nbt.Tag{Type: nbt.List, ListType: nbt.Compound, Value: []nbt.Tag{{Type: nbt.Compound, Value: rider.Data}}}
	if err := w.fallingEntityOperation(0, 0, func(_ *Region, ch *Chunk) error {
		ch.entities = []WorldEntity{survivor, mount}
		ch.indexLivingDamage()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tick := func() {
		t.Helper()
		if err := w.Tick(TickChunks{Entities: func(int32, int32) bool { return true }}); err != nil {
			t.Fatal(err)
		}
	}
	w.addLightning(WorldEntity{Type: LightningEntity, Position: pos}, false, 6)
	tick()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = open()
	actors, err := w.EntitiesInChunk(0, 0)
	if err != nil || len(actors) != 2 {
		t.Fatal(actors, err)
	}
	passengers, err := EntityPassengers(actors[1])
	if err != nil || len(passengers) != 1 {
		t.Fatal(passengers, err)
	}
	for _, e := range []WorldEntity{actors[0], passengers[0]} {
		fire, _ := e.Data.Number("Fire")
		hurt, _ := e.Data.Number("HurtTime")
		if e.Data["Health"].Value != float32(10) || fire != 160 || hurt != 10 {
			t.Fatalf("saved hit lost for %x", e.UUID)
		}
	}
	tick()
	actors, err = w.EntitiesInChunk(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if actors[0].Data["Health"].Value != float32(9) {
		t.Fatal("reload retained transient damage immunity")
	}
	passengers, err = EntityPassengers(actors[1])
	if err != nil || len(passengers) != 1 || !passengers[0].OnFire {
		t.Fatal("new viewer did not see the burning rider", err)
	}
	for i := 1; i < 20; i++ {
		tick()
	}
	actors, err = w.EntitiesInChunk(0, 0)
	if err != nil || len(actors) != 2 || actors[0].UUID != survivor.UUID || actors[1].UUID != rider.UUID {
		t.Fatalf("actors after death: %v (%v)", actors, err)
	}
}
