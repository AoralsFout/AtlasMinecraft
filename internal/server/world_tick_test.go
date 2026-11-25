package server

import (
	"atlas/internal/player"
	"atlas/internal/world"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWorldClockAdvancesAllDimensionsAndCommandsChangeOnlyDayTime(t *testing.T) {
	s, p, _ := dimensionTestServer(t)
	s.Cfg.SimulationDistance = 2
	startAge, _ := s.World.Time()
	if err := s.TickWorlds(); err != nil {
		t.Fatal(err)
	}
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			if _, err := s.World.LoadChunk(x, z); err != nil {
				t.Fatal(err)
			}
		}
	}
	sand, err := world.BlockStateID("minecraft:sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.World.SetBlock(8, -48, 8, sand); err != nil {
		t.Fatal(err)
	}
	if got := s.executeCommand(p, "time set 1d"); !strings.Contains(got, "24000") {
		t.Fatal(got)
	}
	if got := s.executeCommand(p, "time add 0.5s"); !strings.Contains(got, "Added 10 ticks") {
		t.Fatal(got)
	}
	for _, w := range s.Worlds() {
		if age, day := w.Time(); age != startAge+1 || day != 24010 {
			t.Fatalf("%s after time commands: age=%d day=%d", w.DimensionName(), age, day)
		}
	}
	if err := s.TickWorlds(); err != nil {
		t.Fatal(err)
	}
	for _, w := range s.Worlds() {
		if age, day := w.Time(); age != startAge+2 || day != 24011 {
			t.Fatalf("%s after second tick: age=%d day=%d", w.DimensionName(), age, day)
		}
	}
	if got, err := s.World.BlockAt(8, -48, 8); err != nil || got != sand {
		t.Fatalf("sand should still wait for its second game tick: block=%d, err=%v", got, err)
	}
	if err := s.TickWorlds(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.World.BlockAt(8, -48, 8); err != nil || got != world.StateAir {
		t.Fatalf("scheduled sand should become a falling entity: block=%d, err=%v", got, err)
	}
}

func TestWorldClockContinuesAfterDimensionFailure(t *testing.T) {
	s, p, _ := dimensionTestServer(t)
	s.Hub.Leave(p)
	age, day := s.World.Time()
	if err := s.World.Close(); err != nil {
		t.Fatal(err)
	}
	err := s.TickWorlds()
	if !errors.Is(err, world.ErrWorldClosed) || !strings.Contains(err.Error(), "minecraft:overworld") {
		t.Fatalf("expected a dimension-specific error, got %v", err)
	}
	for _, w := range s.Worlds() {
		if w == s.World {
			continue
		}
		if gotAge, gotDay := w.Time(); gotAge != age+1 || gotDay != day+1 {
			t.Fatalf("%s did not tick after overworld failure: age=%d day=%d", w.DimensionName(), gotAge, gotDay)
		}
	}
}

func TestPlayerSimulationFollowsMovementDimensionAndDisconnect(t *testing.T) {
	s, first, _ := dimensionTestServer(t)
	if err := s.World.SetSpawnChunkRadius(0); err != nil {
		t.Fatal(err)
	}
	s.Cfg.SimulationDistance = 2
	second := player.New("Second", [16]byte{2}, 2, nil)
	second.SetPos(16.5, -60, .5, 0, 0, true)
	second.SetPlayReady(true)
	if !s.Hub.Join(second) {
		t.Fatal("second player could not join")
	}
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			if _, err := s.World.LoadChunk(x, z); err != nil {
				t.Fatal(err)
			}
		}
	}
	sand, err := world.BlockStateID("minecraft:sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	placeSand := func(x int32) {
		t.Helper()
		if err := s.World.SetBlock(x, -48, 8, sand); err != nil {
			t.Fatal(err)
		}
	}
	check := func(x int32, want uint32) {
		t.Helper()
		if got, err := s.World.BlockAt(x, -48, 8); err != nil || got != want {
			t.Fatalf("block at %d -48 8: got %d want %d, err=%v", x, got, want, err)
		}
	}
	tick := func(count int) {
		t.Helper()
		for range count {
			if err := s.TickWorlds(); err != nil {
				t.Fatal(err)
			}
		}
	}

	placeSand(8)
	tick(1)
	first.SetPos(160.5, -60, .5, 0, 0, true)
	tick(1)
	check(8, world.StateAir)

	placeSand(9)
	second.ResetWorld("minecraft:the_end", [3]float64{16.5, 100, .5}, 0, 0, nil)
	tick(3)
	check(9, sand)

	second.ResetWorld("minecraft:overworld", [3]float64{16.5, -60, .5}, 0, 0, nil)
	tick(1)
	check(9, world.StateAir)

	placeSand(10)
	s.Hub.Leave(second)
	s.Hub.Leave(first)
	tick(3)
	check(10, sand)
	first.SetPos(.5, -60, .5, 0, 0, true)
	if !s.Hub.Join(first) {
		t.Fatal("player could not rejoin")
	}
	tick(1)
	check(10, world.StateAir)
}

func TestSpawnChunkCommandsAndRestart(t *testing.T) {
	s, p, dir := dimensionTestServer(t)
	s.Hub.Leave(p)
	cmd := func(line, want string) {
		t.Helper()
		if got := s.executeCommand(nil, line); !strings.Contains(got, want) {
			t.Fatalf("%s: %s, want %s", line, got, want)
		}
	}
	cmd("gamerule spawnChunkRadius", "currently set to: 2")
	cmd("execute in minecraft:the_nether run gamerule spawnChunkRadius 0", "now set to: 0")
	s.Cfg.Ops = nil
	if got := s.executeCommand(p, "gamerule spawnChunkRadius 2"); !strings.Contains(got, "permission") {
		t.Fatal(got)
	}
	for _, invalid := range []string{"-1", "33", "1.5"} {
		cmd("gamerule spawnChunkRadius "+invalid, "between 0 and 32")
	}
	cmd("setworldspawn -1599 64 1601", "Set the world spawn")
	if err := s.TickWorlds(); err != nil {
		t.Fatal(err)
	}
	for _, w := range s.Worlds() {
		if n := w.Stats().LoadedChunks; n != 0 {
			t.Fatalf("%s loaded %d with spawn disabled", w.DimensionName(), n)
		}
	}
	cmd("minecraft:gamerule spawnChunkRadius 1", "now set to: 1")
	if err := s.World.Close(); err != nil {
		t.Fatal(err)
	}
	w, err := world.Open(nil, 2, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	if w.SpawnChunkRadius() != 1 {
		t.Fatal("spawn radius lost on restart")
	}
	if err := w.TickPlayers(nil, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	for w.Stats().LoadedChunks != 25 {
		if time.Now().After(deadline) {
			t.Fatalf("restored %d of 25 chunks", w.Stats().LoadedChunks)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := w.ReadLoadedBlocks([]world.BlockPosition{{X: -1599, Y: 64, Z: 1601}}); err != nil {
		t.Fatal("saved spawn was not loaded", err)
	}
	if err := w.SetSpawnChunkRadius(0); err != nil {
		t.Fatal(err)
	}
	if n, err := w.UnloadIdle(0); n != 25 || err != nil {
		t.Fatalf("released %d of 25 chunks, err %v", n, err)
	}
}
