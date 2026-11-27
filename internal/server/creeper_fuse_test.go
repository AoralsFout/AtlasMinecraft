package server

import (
	"atlas/internal/anvil"
	"atlas/internal/nbt"
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"atlas/internal/worldgen"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestCreeperExplosionGameRules(t *testing.T) {
	s, _, _ := dimensionTestServer(t)
	for _, rule := range []string{"mobGriefing", "mobExplosionDropDecay", "doTileDrops"} {
		if got := s.commandGameRule([]string{rule}); got != "Gamerule "+rule+" is currently set to: true" {
			t.Fatal(got)
		}
		for _, value := range []string{"false", "true"} {
			if got := s.commandGameRule([]string{rule, value}); got != "Gamerule "+rule+" is now set to: "+value {
				t.Fatal(got)
			}
			for _, w := range s.Worlds() {
				if w.ExplosionRule(rule) != (value == "true") {
					t.Fatalf("%s: %s=%s", w.DimensionName(), rule, value)
				}
			}
		}
	}
}

func TestCreeperBlocksExplosionPacket(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/worldgen/creeper_blast_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		ItemPackets struct{ Spawn, Metadata string }
		Cases       []struct {
			Power       float32
			Interaction int32
			Affected    int
			Packet      string
		}
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	drop := world.WorldEntity{Type: "minecraft:item", UUID: [16]byte{7: 1, 15: 2},
		Position: [3]float64{-3.25, 200.125, 17.875}, Rotation: [2]float32{45, 0},
		Data: nbt.Object{"Item": {Type: nbt.Compound, Value: nbt.Object{"id": {Type: nbt.String, Value: "minecraft:diamond"}, "count": {Type: nbt.Int, Value: int32(19)}}},
			"Motion": {Type: nbt.List, ListType: nbt.Double, Value: []nbt.Tag{{Type: nbt.Double, Value: -.05}, {Type: nbt.Double, Value: .2}, {Type: nbt.Double, Value: .075}}}}}
	if got := hex.EncodeToString(generatedEntitySpawnPayload(77, drop)); got != ref.ItemPackets.Spawn {
		t.Errorf("item spawn %s, native %s", got, ref.ItemPackets.Spawn)
	}
	if got := hex.EncodeToString(generatedEntityMetadata(77, drop)); got != ref.ItemPackets.Metadata {
		t.Errorf("item metadata %s, native %s", got, ref.ItemPackets.Metadata)
	}
	for _, row := range ref.Cases {
		native, err := hex.DecodeString(row.Packet)
		if err != nil {
			t.Fatal(err)
		}
		r := proto.NewReader(native)
		var pos [3]float64
		for i := range pos {
			pos[i], err = r.F64()
			if err != nil {
				t.Fatal(err)
			}
		}
		event := world.ExplosionEvent{Center: pos, Power: row.Power, BlockInteraction: row.Interaction}
		_, err = r.F32()
		if err != nil {
			t.Fatal(err)
		}
		n, err := r.VarInt()
		if err != nil || int(n) != row.Affected {
			t.Fatalf("affected %d: %v", n, err)
		}
		for range n {
			var xyz [3]int32
			for i := range xyz {
				v, err := r.I8()
				if err != nil {
					t.Fatal(err)
				}
				xyz[i] = int32(math.Floor(pos[i])) + int32(v)
			}
			event.Affected = append(event.Affected, worldgen.FeaturePosition{X: xyz[0], Y: xyz[1], Z: xyz[2]})
		}
		if !bytes.Equal(explosionPacket(&event, 1), native) {
			t.Fatalf("native packet mismatch for power %.0f interaction %d", row.Power, row.Interaction)
		}
	}
}

// Frozen dedicated 1.21.1 server, mobGriefing=false. The player stands 2.75
// blocks east; cows stand on both sides of the damage radius and exactly on it.
func TestCreeperExplosionAgainstDedicatedServer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		radius float64
		health float32
		packet string
	}{
		{"ordinary", 6, 8.964942, "4021000000000000c04d800000000000402100000000000040400000003eeef4253e8cc3f400000000001615c104"},
		{"charged", 12, 8.964971, "4021000000000000c04d800000000000402100000000000040c00000003f2a06553ec851ef00000000001615c104"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, dir := dimensionTestServer(t)
			if err := s.World.SetExplosionRule("mobGriefing", false); err != nil {
				t.Fatal(err)
			}
			s.Cfg.SimulationDistance = 2
			p.SetInitialGameMode(0)
			p.SetPos(11.25, -59, 8.5, 0, 0, true)
			chunks := make(map[[2]int32][]nbt.Tag)
			for i, offset := range [][2]float64{{0, 0}, {tc.radius - .01, 0}, {tc.radius + .01, 0}, {0, tc.radius}} {
				x, z := 8.5+offset[0], 8.5+offset[1]
				d := nbt.Object{
					"id": {Type: nbt.String, Value: "minecraft:cow"}, "UUID": {Type: nbt.IntArray, Value: []int32{0, 0, 0, int32(i + 1)}},
					"Pos":      {Type: nbt.List, ListType: nbt.Double, Value: []nbt.Tag{{Type: nbt.Double, Value: x}, {Type: nbt.Double, Value: -59.0}, {Type: nbt.Double, Value: z}}},
					"Rotation": {Type: nbt.List, ListType: nbt.Float, Value: []nbt.Tag{{Type: nbt.Float, Value: float32(0)}, {Type: nbt.Float, Value: float32(0)}}},
					"Health":   {Type: nbt.Float, Value: float32(10)}, "NoAI": {Type: nbt.Byte, Value: int8(1)},
				}
				if i == 0 {
					d["id"] = nbt.Tag{Type: nbt.String, Value: "minecraft:creeper"}
					d["ignited"] = nbt.Tag{Type: nbt.Byte, Value: int8(1)}
					d["Fuse"] = nbt.Tag{Type: nbt.Short, Value: int16(3)}
					if tc.name == "charged" {
						d["powered"] = nbt.Tag{Type: nbt.Byte, Value: int8(1)}
					}
				}
				chunk := [2]int32{int32(x) >> 4, int32(z) >> 4}
				chunks[chunk] = append(chunks[chunk], nbt.Tag{Type: nbt.Compound, Value: d})
			}
			for chunk, actors := range chunks {
				root := nbt.Object{"DataVersion": {Type: nbt.Int, Value: int32(3955)}, "Position": {Type: nbt.IntArray, Value: chunk[:]}, "Entities": {Type: nbt.List, ListType: nbt.Compound, Value: actors}}
				if err := anvil.WriteChunk(filepath.Join(dir, "entities"), chunk[0], chunk[1], "", nbt.Tag{Type: nbt.Compound, Value: root}); err != nil {
					t.Fatal(err)
				}
			}
			for x := int32(-2); x <= 3; x++ {
				for z := int32(-2); z <= 2; z++ {
					if _, err := s.World.LoadChunk(x, z); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := s.World.TickScheduled(func(x, z int32) bool { return x >= 0 && x <= 1 && z >= 0 && z <= 1 }); err != nil {
				t.Fatal(err)
			}
			p.MarkChunkSent(0, 0)
			s.updateGeneratedEntityVisibility(p)
			for len(p.Out) > 0 {
				<-p.Out
			}
			for tick := 1; tick <= 4; tick++ {
				if err := s.TickWorlds(); err != nil {
					t.Fatal(err)
				}
				var primed, dir, blast, removed bool
				for len(p.Out) > 0 {
					packet := <-p.Out
					r := proto.NewReader(packet.Payload)
					switch packet.ID {
					case proto.SSoundEffect:
						sound, _ := r.VarInt()
						primed = sound == 375 || primed
					case proto.SEntityMetadata:
						_, _ = r.VarInt()
						if hex.EncodeToString(r.Rest()) == "100101ff" {
							dir = true
						}
					case proto.SExplosion:
						if blast || hex.EncodeToString(packet.Payload) != tc.packet {
							t.Fatalf("tick %d explosion %x, native %s", tick, packet.Payload, tc.packet)
						}
						blast = true
					case proto.SEntityDestroy:
						if !blast {
							t.Fatalf("creeper removed before explosion on tick %d", tick)
						}
						removed = true
					}
				}
				if primed != (tick == 1) || dir != (tick == 1) || blast != (tick == 3) || removed != blast {
					t.Fatalf("tick %d: prime %t dir %t blast %t removal %t", tick, primed, dir, blast, removed)
				}
				if tick == 2 {
					late := player.New("Late", [16]byte{7}, 999, nil)
					late.SetPlayReady(true)
					late.SetPos(11.25, -59, 8.5, 0, 0, true)
					late.MarkChunkSent(0, 0)
					s.updateGeneratedEntityVisibility(late)
					id := s.generatedViews[late][generatedEntityKey{s.World.DimensionName(), [16]byte{15: 1}}]
					var fields string
					for len(late.Out) > 0 {
						msg := <-late.Out
						r := proto.NewReader(msg.Payload)
						if entity, _ := r.VarInt(); msg.ID == proto.SEntityMetadata && entity == id {
							fields = hex.EncodeToString(r.Rest())
						}
					}
					want := "0903412000000f0001100101"
					if tc.name == "charged" {
						want += "110801"
					}
					if fields != want+"120801ff" {
						t.Fatal("late observer's fuse metadata", fields)
					}
				}
			}
			count := 0
			for chunk := range chunks {
				entities, err := s.World.EntitiesInChunk(chunk[0], chunk[1])
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range entities {
					want := float32(10)
					if e.UUID[15] == 2 {
						want = tc.health
					}
					if e.UUID[15] == 4 {
						want = 9
					}
					if e.Type != "minecraft:cow" || math.Abs(float64(e.Data["Health"].Value.(float32)-want)) > 1e-6 {
						t.Fatalf("cow %x health %v, native %v", e.UUID, e.Data["Health"], want)
					}
					count++
				}
			}
			if count != 3 {
				t.Fatal("expected three surviving cows", count)
			}
		})
	}
}
