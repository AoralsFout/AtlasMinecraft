package world

import (
	"atlas/internal/light"
	"atlas/internal/registry"
	"atlas/internal/worldgen"
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestPrecipitationColumnsAgainstVanilla(t *testing.T) {
	var scenes []struct {
		Name, Biome string
		Rain        bool
		Cap, Light  int32
		Initial     [][4]int32
		States      [][]uint32
	}
	data, err := os.ReadFile("../../testdata/worldgen/precipitation_1.21.1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &scenes); err != nil {
		t.Fatal(err)
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
			t.Cleanup(func() {
				if err := w.Close(); err != nil {
					t.Error(err)
				}
			})
			w.store.vanilla.flat.layers = nil
			w.store.vanilla.flat.biome, err = registry.BiomeID("minecraft:" + scene.Biome)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.SetSnowAccumulationHeight(scene.Cap); err != nil {
				t.Fatal(err)
			}
			if scene.Rain {
				w.levels.Rain = 1
			}
			cx, cz := scene.Initial[0][0]>>4, scene.Initial[0][2]>>4
			if _, err := w.LoadChunk(cx, cz); err != nil {
				t.Fatal(err)
			}
			points := make([]BlockPosition, len(scene.Initial))
			w.lighting, err = light.New(MinY, WorldHeight, true)
			if err != nil {
				t.Fatal(err)
			}
			var holder *chunkScheduledTicks
			err = w.fallingEntityOperation(cx, cz, func(_ *Region, c *Chunk) error {
				for i, v := range scene.Initial {
					points[i] = BlockPosition{v[0], v[1], v[2]}
					c.SetBlock(int(v[0]&15), int(v[2]&15), v[1], uint32(v[3]))
				}
				holder = c.scheduledTicks
				// Import the original probe's supplied block light, with full daylight.
				sky, block := make([][]byte, NumSections+2), make([][]byte, NumSections+2)
				section := sectionOf(points[0].Y) + 1
				sky[section] = bytes.Repeat([]byte{0xff}, 2048)
				block[section] = bytes.Repeat([]byte{byte(scene.Light * 17)}, 2048)
				_, err := w.lighting.Install(light.Position{X: cx, Z: cz}, denseLightStates(c), &light.Data{Sky: sky, Block: block, Preserved: true})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			r := fluidRuntime{world: w, settings: w.fluidSettings()}
			// Aim the first precipitation draw at the native probe's column. No
			// other chunk or phase consumes either random stream in this harness.
			p := points[0]
			bits := uint32(p.X&15 | (p.Z&15)<<8)
			weatherSeed := int64(0)
			for worldgen.NewLegacyRandom(weatherSeed).NextIntBound(48) != 0 {
				weatherSeed++
			}
			for tick, want := range scene.States {
				w.randomPosition = ((bits << 2) - 1013904223) * 0xaaaaaaab
				w.tickRandom = worldgen.NewLegacyRandom(weatherSeed)
				if err := r.randomTickChunk(holder, 1, nil); err != nil {
					t.Fatal(err)
				}
				got, err := w.ReadLoadedBlocks(points)
				if err != nil || !slices.Equal(got, want) {
					t.Fatalf("sample %d got %v want %v: %v", tick+1, got, want, err)
				}
			}
			if _, err := w.UnloadIdle(0); err != nil {
				t.Fatal(err)
			}
			if _, err := w.LoadChunk(cx, cz); err != nil {
				t.Fatal(err)
			}
			got, err := w.ReadLoadedBlocks(points)
			if err != nil || !slices.Equal(got, scene.States[len(scene.States)-1]) {
				t.Fatalf("reloaded column %v: %v", got, err)
			}
		})
	}
}
