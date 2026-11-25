package world

import (
	"atlas/internal/tickqueue"
	"atlas/internal/worldgen"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScheduledBlockFailureRetainsFluidPhase(t *testing.T) {
	w, _ := mechanicsWorld(t)
	p := worldgen.FeaturePosition{X: 8, Y: 64, Z: 8}
	farmland, err := BlockStateID("minecraft:farmland", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.blockUpdateMu.Lock()
	_, err = w.editBlock(setBlockMsg{x: p.X, y: p.Y, z: p.Z, state: farmland, raw: true, flags: 2})
	if err == nil {
		err = w.scheduledTicks.scheduleBlock(p, "minecraft:farmland", 0, 0)
	}
	if err == nil {
		err = w.scheduledTicks.schedule(worldgen.FeaturePosition{X: 9, Y: 64, Z: 8}, worldgen.FluidWater, 0)
	}
	w.blockUpdateMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	eligible := func(x, z int32) bool { return x == 0 && z == 0 }
	age, daytime := w.Time()
	if err := w.Tick(TickChunks{}); err != nil {
		t.Fatal("time-only tick ran an unsupported block handler", err)
	}
	if err := w.Tick(TickChunks{Blocks: eligible}); !errors.Is(err, worldgen.ErrUnsupportedScheduledBlock) {
		t.Fatal("unsupported block tick did not fail explicitly", err)
	}
	if gotAge, gotDaytime := w.Time(); gotAge != age+2 || gotDaytime != daytime+2 {
		t.Fatalf("time after two ticks: age=%d daytime=%d, want %d/%d", gotAge, gotDaytime, age+2, daytime+2)
	}
	o := w.scheduledTicks
	o.mu.Lock()
	blocks, fluids := o.blocks.Len(), o.level.Len()
	o.mu.Unlock()
	if blocks != 1 || fluids != 1 {
		t.Fatal("failure consumed block/fluid work", blocks, fluids)
	}
	// Replacing the unsupported block makes its request stale; the next pass
	// then drains both stale entries without invoking a different block's tick.
	w.blockUpdateMu.Lock()
	_, err = w.editBlock(setBlockMsg{x: p.X, y: p.Y, z: p.Z, state: StateStone, raw: true, flags: 2})
	w.blockUpdateMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.TickScheduled(eligible); err != nil {
		t.Fatal(err)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.blocks.Len() != 0 || o.level.Len() != 0 || o.chunks[tickqueue.ChunkKey(0, 0)].blocks.Len() != 0 {
		t.Fatal("stale scheduled entries survived retry")
	}
}

func TestColdBlockEditDoesNotBlockWorldClock(t *testing.T) {
	loading, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	w := testGenerationWorld(1, func(x, z int32) (*Chunk, error) {
		close(loading)
		<-release
		return NewChunk(x, z), nil
	})
	t.Cleanup(func() {
		unblock.Do(func() { close(release) })
		w.Close()
	})
	edited := make(chan error, 1)
	go func() { edited <- w.SetBlock(128, 64, 0, StateStone) }()
	select {
	case <-loading:
	case <-time.After(3 * time.Second):
		t.Fatal("cold edit did not request terrain")
	}
	ticked := make(chan error, 1)
	go func() { ticked <- w.Tick(TickChunks{}) }()
	select {
	case err := <-ticked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("world clock waited for remote terrain generation")
	}
	unblock.Do(func() { close(release) })
	if err := <-edited; !errors.Is(err, ErrVanillaGenerationUnavailable) {
		t.Fatalf("test world does not persist edits, got %v", err)
	}
}

func TestLiveTickRetriesFailedBlockWhileSandAndWaterAdvance(t *testing.T) {
	w, _ := mechanicsWorld(t)
	centers := []SimulationPlayer{{X: 8, Z: 8}}
	water, err := BlockStateID("minecraft:water", nil)
	if err != nil {
		t.Fatal(err)
	}
	for z := int32(-1); z <= 1; z++ {
		if _, err := w.LoadChunk(2, z); err != nil {
			t.Fatal(err)
		}
	}
	for _, wall := range []BlockPosition{{14, -45, 8}, {16, -45, 8}, {15, -45, 7}, {15, -45, 9}, {15, -46, 8}} {
		if err := w.SetBlock(wall.X, wall.Y, wall.Z, StateStone); err != nil {
			t.Fatal(err)
		}
	}
	// A settled source gains a pending tick when the barrier in the next chunk is dug.
	w.blockUpdateMu.Lock()
	_, err = w.editBlock(setBlockMsg{x: 15, y: -45, z: 8, state: water, raw: true, flags: 2})
	w.blockUpdateMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.TryPlaceBlock(BlockPosition{16, -45, 8}, StateStone, StateAir, nil); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := w.TickPlayers(centers, 1); err != nil {
			t.Fatal(err)
		}
	}
	farmland, err := BlockStateID("minecraft:farmland", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetBlock(9, 64, 9, farmland); err != nil {
		t.Fatal(err)
	}
	if err := w.scheduledTicks.scheduleBlock(worldgen.FeaturePosition{X: 9, Y: 64, Z: 9}, "minecraft:farmland", 2, 0); err != nil {
		t.Fatal(err)
	}
	sand, err := BlockStateID("minecraft:sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetBlock(12, -48, 8, sand); err != nil {
		t.Fatal(err)
	}
	if err := w.TickPlayers(centers, 1); err != nil {
		t.Fatal(err)
	}
	err = w.TickPlayers(centers, 1)
	if !errors.Is(err, worldgen.ErrUnsupportedScheduledBlock) || !strings.Contains(err.Error(), "minecraft:farmland at 9,64,9") {
		t.Fatalf("live tick should report the failed request, got %v", err)
	}
	flow, err := BlockStateID("minecraft:water", map[string]string{"level": "1"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(x, y, z int32, want uint32) {
		t.Helper()
		if got, err := w.BlockAt(x, y, z); err != nil || got != want {
			t.Fatalf("block at %d,%d,%d: got %d want %d, err=%v", x, y, z, got, want, err)
		}
	}
	check(12, -48, 8, StateAir)
	check(16, -45, 8, flow)
	w.scheduledTicks.mu.Lock()
	queued := w.scheduledTicks.blocks.Has(tickqueue.Position{X: 9, Y: 64, Z: 9}, "minecraft:farmland")
	w.scheduledTicks.mu.Unlock()
	if !queued {
		t.Fatal("failed request was lost")
	}
	if err := w.SetBlock(15, -45, 8, StateAir); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := w.TickPlayers(centers, 1); err != nil {
			t.Fatal("failed request was retried before its delay", err)
		}
	}
	check(16, -45, 8, StateAir)
	for range simulationRetryDelay - 6 {
		if err := w.TickPlayers(centers, 1); err != nil {
			t.Fatal("failed request was retried before its delay", err)
		}
	}
	if err := w.TickPlayers(centers, 1); !errors.Is(err, worldgen.ErrUnsupportedScheduledBlock) {
		t.Fatalf("retained failure was not retried, got %v", err)
	}
}

func TestLiveLeafDistanceUpdatesFlushAtWorldSave(t *testing.T) {
	w, _ := mechanicsWorld(t)
	const x, y, z = 8, 64, 8
	state := func(name string, properties map[string]string) uint32 {
		t.Helper()
		id, err := BlockStateID(name, properties)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	before := state("oak_leaves", map[string]string{"distance": "7"})
	after := state("oak_leaves", map[string]string{"distance": "1"})
	if err := w.SetBlock(x, y, z, before); err != nil {
		t.Fatal(err)
	}
	if err := w.SetBlock(x-1, y, z, state("oak_log", nil)); err != nil {
		t.Fatal(err)
	}
	if err := w.TickPlayers([]SimulationPlayer{{X: 8, Z: 8}}, 1); err != nil {
		t.Fatal(err)
	}
	if got, err := w.BlockAt(x, y, z); err != nil || got != after {
		t.Fatalf("leaf distance after tick: got %d want %d, err=%v", got, after, err)
	}
	checkSaved := func(want uint32) {
		t.Helper()
		chunk, err := w.store.vanilla.loadBlockChunk(0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := chunk.GetBlock(x, z, y); got != want {
			t.Fatalf("saved leaf state: got %d want %d", got, want)
		}
	}
	checkSaved(before)
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	checkSaved(after)
}

func TestLogRemovalSchedulesLeavesInShapeOrder(t *testing.T) {
	w, _ := mechanicsWorld(t)
	logID, err := BlockStateID("oak_log", nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := BlockStateID("oak_leaves", map[string]string{"distance": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetBlock(8, 64, 8, logID); err != nil {
		t.Fatal(err)
	}
	// Vanilla updateNeighbourShapes visits west, east, north, south, down, up.
	want := []tickqueue.Position{{X: 7, Y: 64, Z: 8}, {X: 9, Y: 64, Z: 8}, {X: 8, Y: 64, Z: 7}, {X: 8, Y: 64, Z: 9}, {X: 8, Y: 63, Z: 8}, {X: 8, Y: 65, Z: 8}}
	for _, p := range want {
		if err := w.SetBlock(p.X, p.Y, p.Z, leaf); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.TryPlaceBlock(BlockPosition{8, 64, 8}, logID, StateAir, nil); err != nil {
		t.Fatal(err)
	}
	var got []tickqueue.Position
	w.scheduledTicks.observeDispatch = func(_ int64, kind string, p tickqueue.Position, name string) {
		if kind == "block" && name == "minecraft:oak_leaves" {
			got = append(got, p)
		}
	}
	if err := w.Tick(TickChunks{Blocks: func(x, z int32) bool { return x == 0 && z == 0 }}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("leaf tick order = %v, want %v", got, want)
	}
}

func TestPressedButtonKeepsRemainingTicksAcrossRestart(t *testing.T) {
	w, dir := mechanicsWorld(t)
	pos := BlockPosition{8, -60, 8}
	id, err := BlockStateID("minecraft:stone_button", map[string]string{"face": "floor"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetBlock(pos.X, pos.Y, pos.Z, id); err != nil {
		t.Fatal(err)
	}
	if err := w.PressStoneButton(pos, id); err != nil {
		t.Fatal(err)
	}
	saved := diskBlockTicks(t, dir, 0, 0)
	if len(saved) != 1 || saved[0].Type != "minecraft:stone_button" || saved[0].Delay != 20 {
		t.Fatalf("button press saved without its release: %+v", saved)
	}
	eligible := TickChunks{Blocks: func(x, z int32) bool { return x == 0 && z == 0 }}
	for range 7 {
		if err := w.Tick(eligible); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	saved = diskBlockTicks(t, dir, 0, 0)
	if len(saved) != 1 || saved[0].Delay != 13 {
		t.Fatalf("remaining button ticks: %+v", saved)
	}
	reopened, err := Open(noViewers{}, 1, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			if _, err := reopened.LoadChunk(x, z); err != nil {
				t.Fatal(err)
			}
		}
	}
	for range 4 {
		if err := reopened.Tick(TickChunks{}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 13; i++ {
		if err := reopened.Tick(eligible); err != nil {
			t.Fatal(err)
		}
		got, err := reopened.BlockAt(pos.X, pos.Y, pos.Z)
		if err != nil || (got == id) != (i == 13) {
			t.Fatalf("resumed tick %d, state %d, error %v", i, got, err)
		}
	}
}
