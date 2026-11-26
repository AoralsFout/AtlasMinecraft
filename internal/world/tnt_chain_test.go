package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

func TestTNTChainTicksAgainstDedicatedServer(t *testing.T) {
	type trajectory struct{ Pos, Motion [3]float64 }
	var ref struct {
		Scenarios []struct {
			Seed   int64
			Decay  bool
			Blocks [][4]int32
			Ticks  []struct {
				Tick      int
				Explosion *struct {
					Center      [3]float64
					Power       float32
					Interaction int32
				}
				TNT []struct {
					trajectory
					Fuse int64
				}
				Items []struct {
					trajectory
					Alive  bool
					Health int64
				}
			}
		}
	}
	data, err := os.ReadFile("../../testdata/worldgen/tnt_chain_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &ref); err != nil || len(ref.Scenarios) != 3 {
		t.Fatalf("native TNT scenes: %v", err)
	}
	for _, row := range ref.Scenarios {
		t.Run(fmt.Sprint(row.Seed), func(t *testing.T) {
			dir := t.TempDir()
			seed := int64(1234)
			if err := CreateFlatWorld(dir, &seed); err != nil {
				t.Fatal(err)
			}
			w, err := Open(noViewers{}, 1, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			// Vanilla's probe steps only the entity phase. Keep weather out of the shared RNG.
			if err := w.SetWeatherCycle(false); err != nil {
				t.Fatal(err)
			}
			if w.ExplosionRule("tntExplosionDropDecay") {
				t.Fatal("TNT drop decay defaults off")
			}
			for name, value := range map[string]bool{"mobExplosionDropDecay": false, "tntExplosionDropDecay": row.Decay} {
				if err := w.SetExplosionRule(name, value); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := w.LoadChunk(0, 0); err != nil {
				t.Fatal(err)
			}
			if err := w.fallingEntityOperation(0, 0, func(_ *Region, ch *Chunk) error {
				for _, p := range row.Blocks {
					ch.SetBlock(int(p[0]), int(p[2]), p[1], uint32(p[3]))
				}
				ch.entities = []WorldEntity{{Type: "minecraft:creeper", Position: [3]float64{7.5, 200, 8.5}, UUID: [16]byte{7}, Data: nbt.Object{
					"Health": {Type: nbt.Float, Value: float32(20)}, "Fuse": shortEntityTag(1), "ignited": byteTag(1), "NoAI": byteTag(1),
				}}}
				ch.indexLivingDamage()
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			w.tickRandom = worldgen.NewLegacyRandom(row.Seed)
			var events []*ExplosionEvent
			var firstDrop [16]byte
			chunks := TickChunks{Entities: func(x, z int32) bool { return x == 0 && z == 0 }, livingChanged: func(c LivingDamageChange) {
				if c.Explosion != nil {
					events = append(events, c.Explosion)
				}
				if c.Spawned && c.Entity.Type == "minecraft:item" && firstDrop == ([16]byte{}) {
					firstDrop = c.Entity.UUID
				}
			}}
			for tick, want := range row.Ticks {
				if want.Tick != tick || len(want.Items) != 1 {
					t.Fatalf("incomplete native tick %d", tick)
				}
				events = events[:0]
				if err := w.Tick(chunks); err != nil {
					t.Fatalf("tick %d: %v", tick, err)
				}
				assertVector := func(field string, got, want [3]float64) {
					t.Helper()
					for i := range got {
						if math.Abs(got[i]-want[i]) > 1e-9 {
							t.Fatalf("tick %d %s = %v, native %v", tick, field, got, want)
						}
					}
				}
				if want.Explosion == nil {
					if len(events) != 0 {
						t.Fatalf("tick %d unexpected explosion %+v", tick, events[0])
					}
				} else {
					if len(events) != 1 || events[0].Power != want.Explosion.Power || events[0].BlockInteraction != want.Explosion.Interaction {
						t.Fatalf("tick %d explosion %v, native %+v", tick, events, want.Explosion)
					}
					assertVector("explosion center", events[0].Center, want.Explosion.Center)
				}
				entities, err := w.EntitiesInChunk(0, 0)
				if err != nil {
					t.Fatal(err)
				}
				tntCount := 0
				alive := false
				for _, e := range entities {
					if e.Type == TNTEntity {
						fuse, _ := e.Data.Number("fuse")
						if tntCount >= len(want.TNT) || fuse != want.TNT[tntCount].Fuse {
							t.Fatalf("tick %d unexpected TNT fuse %d, native %+v", tick, fuse, want.TNT)
						}
						assertVector("TNT position", e.Position, want.TNT[tntCount].Pos)
						assertVector("TNT motion", entityMotion(e), want.TNT[tntCount].Motion)
						tntCount++
					}
					if e.UUID == firstDrop {
						alive = true
						stack, _ := e.Data.Object("Item")
						name, _ := stack.Text("id")
						count, _ := stack.Number("count")
						if name != "minecraft:dirt" || count != 1 {
							t.Fatalf("tick %d unexpected first drop %s x%d", tick, name, count)
						}
						if health, _ := e.Data.Number("Health"); health != want.Items[0].Health {
							t.Fatalf("tick %d first drop health %d, native %d", tick, health, want.Items[0].Health)
						}
						assertVector("first drop position", e.Position, want.Items[0].Pos)
						assertVector("first drop motion", entityMotion(e), want.Items[0].Motion)
					}
					if e.Type == "minecraft:item" && tick == 0 && e.UUID != firstDrop {
						t.Fatal("unexpected extra item from the first blast")
					}
				}
				if tntCount != len(want.TNT) || alive != want.Items[0].Alive {
					t.Fatalf("tick %d TNT count %d (native %d), first drop alive %t (native %t)", tick, tntCount, len(want.TNT), alive, want.Items[0].Alive)
				}
			}
		})
	}
}
