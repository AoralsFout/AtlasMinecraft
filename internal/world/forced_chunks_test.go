package world

import (
	"atlas/internal/javasave"
	"atlas/internal/nbt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestForcedChunkDataAgainstVanilla(t *testing.T) {
	dir := t.TempDir()
	seed := int64(0)
	if err := CreateFlatWorld(dir, &seed); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("../../testdata/worldgen/forced_chunks_1.21.1.dat")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "data", "chunks.dat")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, src, 0644); err != nil {
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
	first := ChunkPosition{-100, 100}
	if err := w.SetSpawnChunkRadius(0); err != nil {
		t.Fatal(err)
	}
	second := ChunkPosition{-99, 100}
	if got := w.ForcedChunks(); !slices.Equal(got, []ChunkPosition{first}) {
		t.Fatalf("original centers %v", got)
	}
	// Overlap a player ticket with both forced centers. Removing one must
	// retain the other's radius and the independently ticking player chunks.
	if _, err := w.SetChunksForced(second, second, true); err != nil {
		t.Fatal(err)
	}
	if err := w.TickPlayers([]SimulationPlayer{{X: float64(first.X * 16), Z: float64(first.Z * 16)}}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetChunksForced(first, first, false); err != nil {
		t.Fatal(err)
	}
	key := chunkKey{first.X, first.Z}
	if level := w.simulation.Load().chunks[key].level; level > 31 {
		t.Fatalf("lost player ticket: level %d", level)
	}
	if err := w.TickPlayers(nil, 10); err != nil {
		t.Fatal(err)
	}
	snap := w.simulation.Load()
	if len(snap.chunks) != 25 || snap.chunks[key].level != 32 || snap.chunks[chunkKey{second.X + 2, second.Z}].level != 33 {
		t.Fatalf("remaining forced rings: %+v", snap.chunks)
	}
	b, err := javasave.ReadGzip(path)
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := nbt.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	obj, _ := root.Object()
	data, _ := obj.Object("data")
	values, ok := data["Forced"].Longs()
	if !ok || !slices.Equal(values, []int64{100<<32 | 0xffffff9d}) {
		t.Fatalf("unexpected saved longs: %v", values)
	}
}

func TestForceLoadRangeAndPersistenceFailure(t *testing.T) {
	w := &World{done: make(chan struct{})}
	from, to := ChunkPosition{worldChunkLimit - 1, 0}, ChunkPosition{worldChunkLimit - 16, 15}
	if n, err := w.SetChunksForced(from, to, true); err != nil || n != 256 {
		t.Fatalf("256 chunk inclusive range: %d %v", n, err)
	}
	if level := w.simulation.Load().chunks[chunkKey{worldChunkLimit + 1, 0}].level; level != 33 {
		t.Fatalf("border support ring: level %d", level)
	}
	extra := ChunkPosition{0, 0}
	if n, err := w.SetChunksForced(extra, extra, true); err != nil || n != 1 {
		t.Fatalf("additional center: %d %v", n, err)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("occupied"), 0644); err != nil {
		t.Fatal(err)
	}
	w.store = &Store{vanilla: &VanillaSave{dir: blocked}}
	snapshot := w.simulation.Load()
	if n, err := w.ClearForcedChunks(); err == nil || n != 0 || len(w.ForcedChunks()) != 257 || w.simulation.Load() != snapshot {
		t.Fatalf("failed save changed live tickets: %d %v", n, err)
	}
}
