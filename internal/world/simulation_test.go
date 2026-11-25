package world

import (
	"testing"
	"time"
)

func TestPlayerSimulationLoadsAndRetainsTickingNeighborhoods(t *testing.T) {
	w, _ := mechanicsWorld(t)
	if err := w.SetSpawnChunkRadius(0); err != nil {
		t.Fatal(err)
	}
	centers := []SimulationPlayer{{X: 8, Z: 8}}
	if err := w.TickPlayers(centers, 1); err != nil {
		t.Fatal(err)
	}
	// No chunk requests are sent by a client. The tickets should asynchronously
	// load the entity square, the block-ticking ring and the outer loaded ring.
	deadline := time.Now().Add(45 * time.Second)
	for w.Stats().LoadedChunks != 49 {
		if time.Now().After(deadline) {
			t.Fatalf("simulation prepared %d chunks, want 49", w.Stats().LoadedChunks)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if count, err := w.UnloadIdle(0); err != nil || count != 0 {
		t.Fatalf("unload with active tickets: count=%d err=%v", count, err)
	}
	sand, err := BlockStateID("minecraft:sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int32{1, 2, 3} {
		if err := w.SetBlock(offset*16+8, -48, offset*16+8, sand); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := w.TickPlayers(centers, 1); err != nil {
			t.Fatal(err)
		}
	}
	for offset, wantAge := range map[int32]int64{1: 1, 2: 0} {
		entities, err := w.EntitiesInChunk(offset, offset)
		if err != nil || len(entities) != 1 {
			t.Fatalf("ring %d: falling entities=%d err=%v", offset, len(entities), err)
		}
		if age, ok := entities[0].Data.Number("Time"); !ok || age != wantAge {
			t.Fatalf("ring %d: falling entity age=%d want=%d", offset, age, wantAge)
		}
	}
	if states, err := w.ReadLoadedBlocks([]BlockPosition{{56, -48, 56}}); err != nil || states[0] != sand {
		t.Fatalf("loaded-only ring should retain its sand: states=%v err=%v", states, err)
	}
	if err := w.TickPlayers(nil, 1); err != nil {
		t.Fatal(err)
	}
	if count, err := w.UnloadIdle(0); err != nil || count != 49 {
		t.Fatalf("unload after final ticket: count=%d err=%v", count, err)
	}
}

func TestRandomTickPlayerDistance(t *testing.T) {
	w, _ := mechanicsWorld(t)
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			if _, err := w.LoadChunk(x, z); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The spectator supplies a ticket near the target, never proximity.
	// A moving non-spectator crosses the 128-block boundary inside chunk -8.
	players := []SimulationPlayer{{X: 8, Z: 8, Spectator: true}, {X: -120, Z: 8}}
	check := func(want bool) {
		t.Helper()
		if err := w.TickPlayers(players, 1); err != nil {
			t.Fatal(err)
		}
		if got := w.RandomTickEligible(0, 0); got != want {
			t.Fatalf("players %+v: got %t, want %t", players, got, want)
		}
	}
	check(false)
	tickets := w.simulation.Load()
	players[1].X += .01
	check(true)
	if w.simulation.Load() != tickets {
		t.Fatal("movement inside one chunk rebuilt terrain tickets")
	}
	players[1].Z += 2
	check(false)
	players[1].Spectator = true
	players[1].X, players[1].Z = 8, 8
	check(false)
	players[1].Spectator = false
	check(true)
	if w.RandomTickEligible(2, 0) {
		t.Fatal("block-only ring received a random tick")
	}
	players = nil
	check(false)
}
