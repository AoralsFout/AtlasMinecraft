package world

import (
	"atlas/internal/nbt"
	"encoding/json"
	"os"
	"testing"
)

func TestLightningCreeperChargeAgainstVanilla(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/lightning_creepers_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenes []struct {
		Name         string
		SavedPowered bool
		Ticks        []struct {
			Powered, Burning bool
			Health           float32
			Dirty            string
		}
	}
	if err := json.Unmarshal(raw, &scenes); err != nil || len(scenes) != 6 {
		t.Fatalf("creeper reference: %d cases, %v", len(scenes), err)
	}
	for _, scene := range scenes {
		t.Run(scene.Name, func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(1)
			if err := CreateFlatWorld(dir, &seed); err != nil {
				t.Fatal(err)
			}
			open := func() *World {
				w, err := Open(noViewers{}, 1, dir)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := w.Close(); err != nil {
						t.Error(err)
					}
				})
				if _, err := w.LoadChunk(-3, 2); err != nil {
					t.Fatal(err)
				}
				return w
			}
			w := open()
			w.weatherStopped = true
			if err := w.SetFireTick(false); err != nil {
				t.Fatal(err)
			}
			pos := [3]float64{-40.5, 70, 40.5}
			initial := pos
			if scene.Name == "enters_range" {
				initial[2] += 5
			}
			id := [16]byte{9}
			d := nbt.Object{
				"id": {Type: nbt.String, Value: "minecraft:creeper"}, "UUID": entityUUIDTag(id), "Pos": entityPositionTag(initial),
				"Rotation": {Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: float32(0)}, {Type: nbt.Float, Value: float32(0)}}},
				"Health":   {Type: nbt.Float, Value: float32(20)}, "NoAI": byteTag(1), "Fire": {Type: nbt.Short, Value: int16(0)},
			}
			switch scene.Name {
			case "fresh":
				d["Fire"] = nbt.Tag{Type: nbt.Short, Value: int16(-1)}
			case "invulnerable":
				d["Invulnerable"] = byteTag(1)
			case "precharged":
				d["powered"] = byteTag(1)
			}
			mob, err := worldEntityFromNBT(d)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.fallingEntityOperation(-3, 2, func(_ *Region, ch *Chunk) error {
				ch.entities = []WorldEntity{mob}
				ch.entitiesDirty = true
				ch.indexLivingDamage()
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			w.addLightning(WorldEntity{Type: LightningEntity, Position: pos}, scene.Name == "visual", 6)
			powered := mob.IsChargedCreeper()
			for i, row := range scene.Ticks {
				if i == 3 && scene.Name == "enters_range" {
					if err := w.fallingEntityOperation(-3, 2, func(_ *Region, ch *Chunk) error {
						e := &ch.entities[0]
						e.Position = pos
						e.Data["Pos"] = entityPositionTag(pos)
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				changes, charges := 0, 0
				err := w.Tick(TickChunks{Entities: func(int32, int32) bool { return true }, livingChanged: func(c LivingDamageChange) {
					changes++
					if c.CreeperCharged {
						charges++
					}
					if c.Entity.UUID != id || c.Spawned || c.Removed {
						t.Errorf("unexpected replacement on tick %d", i+1)
					}
				}})
				if err != nil {
					t.Fatal(err)
				}
				entities, err := w.EntitiesInChunk(-3, 2)
				if err != nil || len(entities) != 1 {
					t.Fatalf("creeper snapshot: %v %v", entities, err)
				}
				e := entities[0]
				wantCharges := 0
				if !powered && row.Powered {
					wantCharges = 1
				}
				if charges != wantCharges || (changes > 0) != (row.Dirty != "") || e.IsChargedCreeper() != row.Powered || e.OnFire != row.Burning || e.Data["Health"].Value != row.Health {
					t.Fatalf("tick %d: powered=%v, burning=%v, hp=%v, %d changes (%d charges); native %+v", i+1, e.IsChargedCreeper(), e.OnFire, e.Data["Health"].Value, changes, charges, row)
				}
				powered = row.Powered
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			actors, err := open().EntitiesInChunk(-3, 2)
			if err != nil || len(actors) != 1 || actors[0].UUID != id || actors[0].IsChargedCreeper() != scene.SavedPowered {
				t.Fatalf("reloaded creeper: %v (%v)", actors, err)
			}
		})
	}
}
