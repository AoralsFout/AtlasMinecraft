package world

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSpawnChunkRingsAndEmptyTickPhases(t *testing.T) {
	w, _ := mechanicsWorld(t)
	if radius := w.SpawnChunkRadius(); radius != 2 {
		t.Fatalf("default radius %d", radius)
	}
	tick := func(n int) {
		t.Helper()
		for range n {
			if err := w.TickPlayers(nil, 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	tick(1)
	deadline := time.Now().Add(time.Minute)
	for w.Stats().LoadedChunks != 49 {
		if time.Now().After(deadline) {
			t.Fatalf("loaded %d of 49 spawn chunks", w.Stats().LoadedChunks)
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap := w.simulation.Load()
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			ticket, ok := snap.chunks[chunkKey{x, z}]
			if want := 30 + max(x, -x, z, -z); !ok || ticket.level != want {
				t.Fatalf("spawn chunk [%d,%d] level %d, want %d", x, z, ticket.level, want)
			}
		}
	}
	if w.RandomTickEligible(0, 0) {
		t.Fatal("spawn ticket enabled random ticks without a player")
	}
	sand, err := BlockStateID("sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	place := func(cx int32) {
		t.Helper()
		if err := w.SetBlock(16*cx+8, 80, 8, sand); err != nil {
			t.Fatal(err)
		}
	}
	age := func(cx int32, want int64) {
		t.Helper()
		list, err := w.EntitiesInChunk(cx, 0)
		if err != nil || len(list) != 1 {
			t.Fatalf("chunk %d falling entities %d: %v", cx, len(list), err)
		}
		if got, ok := list[0].Data.Number("Time"); !ok || got != want {
			t.Fatalf("chunk %d falling age %d, want %d", cx, got, want)
		}
	}
	tick(297)
	place(1)
	place(2)
	place(3)
	tick(2) // scheduled sand becomes an entity on empty tick 300
	age(1, 1)
	age(2, 0)
	if got, err := w.BlockAt(56, 80, 8); err != nil || got != sand {
		t.Fatalf("load-only ring executed block tick: %d, %v", got, err)
	}
	place(0)
	tick(2)
	age(0, 0) // scheduled block ran during the entity pause
	age(1, 1)
	p := ChunkPosition{0, 0}
	if _, err := w.SetChunksForced(p, p, true); err != nil {
		t.Fatal(err)
	}
	tick(1)
	age(0, 1)
	age(1, 2)
}

func TestSpawnTicketMovesAndPreservesOtherSources(t *testing.T) {
	w, _ := mechanicsWorld(t)
	players := []SimulationPlayer{{X: -32, Z: 0}}
	if err := w.TickPlayers(players, 0); err != nil {
		t.Fatal(err)
	}
	forced := ChunkPosition{1, 0}
	if _, err := w.SetChunksForced(forced, forced, true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetSpawn(-1599, 64, 1601, 0); err != nil {
		t.Fatal(err)
	}
	check := func(key chunkKey, want int32) {
		t.Helper()
		ticket, ok := w.simulation.Load().chunks[key]
		if (want != 0) != ok || ticket.level != want {
			t.Fatalf("chunk %v got level %d (present %v), want %d", key, ticket.level, ok, want)
		}
	}
	check(chunkKey{-100, 100}, 30)
	check(chunkKey{1, 0}, 31)
	check(chunkKey{-2, 0}, 31)
	check(chunkKey{0, 3}, 0)
	old := w.simulation.Load()
	if _, err := w.SetSpawn(-1600, 72, 1600, 90); err != nil || w.simulation.Load() != old {
		t.Fatalf("move inside spawn chunk rebuilt tickets: %v", err)
	}
	if err := w.SetSpawnChunkRadius(1); err != nil {
		t.Fatal(err)
	}
	check(chunkKey{-100, 100}, 31)
	check(chunkKey{-103, 100}, 0)
	if err := w.SetSpawnChunkRadius(0); err != nil {
		t.Fatal(err)
	}
	check(chunkKey{-100, 100}, 0)
	check(chunkKey{1, 0}, 31)
	check(chunkKey{-2, 0}, 31)
}

func TestSpawnTicketSaveFailureKeepsPriorSettings(t *testing.T) {
	// A level.dat path below a missing directory forces the atomic write
	// to fail. No generation worker is needed to inspect the published set.
	dim, _ := builtinDimension("overworld")
	v := &VanillaSave{dir: filepath.Join(t.TempDir(), "missing"), dimension: dim, root: newWorldLevel("test", 0, true)}
	w := &World{store: &Store{vanilla: v}, spawnRadius: 2, done: make(chan struct{})}
	previous := w.publishSimulationTickets(nil, 0)
	if err := w.SetSpawnChunkRadius(1); err == nil {
		t.Fatal("expected failed radius save")
	}
	if changed, err := w.SetSpawn(1600, 64, 0, 0); err == nil || changed {
		t.Fatalf("failed spawn save: changed=%v err=%v", changed, err)
	}
	x, z := w.SpawnXZ()
	radius, err := v.savedSpawnRadius()
	if err != nil || radius != 2 || w.SpawnChunkRadius() != 2 || x != 0 || z != 0 || w.simulation.Load() != previous {
		t.Fatal("failed write changed the live spawn settings")
	}
}
