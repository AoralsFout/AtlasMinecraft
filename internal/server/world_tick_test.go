package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func drainClockPackets(t *testing.T, p *player.Player) [][2]int64 {
	t.Helper()
	var times [][2]int64
	for len(p.Out) > 0 {
		packet := <-p.Out
		if packet.ID != proto.SUpdateTime {
			continue
		}
		r := proto.NewReader(packet.Payload)
		age, err := r.I64()
		if err != nil {
			t.Fatal(err)
		}
		day, err := r.I64()
		if err != nil || r.Remaining() != 0 {
			t.Fatalf("bad time packet %x", packet.Payload)
		}
		times = append(times, [2]int64{age, day})
	}
	return times
}

func TestSharedDaylightCycleAgainstVanilla(t *testing.T) {
	s, p, dir := dimensionTestServer(t)
	s.Cfg.SimulationDistance = 1
	command := func(line, want string) {
		t.Helper()
		if got := s.executeCommand(nil, line); !strings.Contains(got, want) {
			t.Fatalf("%s: %s", line, got)
		}
	}
	check := func(age, day int64, cycle bool) {
		t.Helper()
		for _, w := range s.Worlds() {
			a, d, c := w.TimeState()
			if a != age || d != day || c != cycle {
				t.Fatalf("%s time=(%d,%d,%t), want (%d,%d,%t)", w.DimensionName(), a, d, c, age, day, cycle)
			}
		}
	}
	tick := func() {
		t.Helper()
		if err := s.TickWorlds(); err != nil {
			t.Fatal(err)
		}
	}
	check(0, 0, true)
	tick()
	command("time set 23999", "23999")
	check(1, 23999, true)
	tick()
	check(2, 24000, true)
	command("execute in minecraft:the_end run gamerule doDaylightCycle false", "now set to: false")
	tick()
	check(3, 24000, false)
	command("execute in minecraft:the_nether run time add 3", "Added 3 ticks")
	check(3, 24003, false)
	command("execute in minecraft:the_end run time query day", "Day: 1")
	command("time set 0", "Time set to 0")
	s.sendWorldState(p)
	expectPacket := func(want [2]int64) {
		t.Helper()
		got := drainClockPackets(t, p)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("time packets %v, want %v", got, want)
		}
	}
	expectPacket([2]int64{3, -1})
	command("time set 42", "Time set to 42")
	for _, dim := range []string{"minecraft:the_nether", "minecraft:the_end", "minecraft:overworld"} {
		if err := s.TeleportToDimension(p, dim, [3]float64{0, 80, 0}); err != nil {
			t.Fatal(err)
		}
		expectPacket([2]int64{3, -42})
	}
	command("gamerule doDaylightCycle true", "now set to: true")
	tick()
	check(4, 43, true)
	command("gamerule doDaylightCycle 1", "expected true or false")
	command("gamerule doDaylightCycle", "currently set to: true")
	command("gamerule doDaylightCycle false", "now set to: false")
	command("save-all flush", "World saved")
	for _, dim := range s.Worlds() {
		w, err := world.OpenDimension(nil, 1, dir, dim.DimensionName())
		if err != nil {
			t.Fatal(err)
		}
		a, d, c := w.TimeState()
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if a != 4 || d != 43 || c {
			t.Fatalf("restored %s: %d,%d,%t", dim.DimensionName(), a, d, c)
		}
	}
}

func TestPeriodicTimePacketsFollowDimensionTickOrder(t *testing.T) {
	s, ow, _ := dimensionTestServer(t)
	s.Cfg.SimulationDistance = 1
	peers := []*player.Player{ow}
	for i, dim := range []string{"minecraft:the_nether", "minecraft:the_end"} {
		p := player.New(dim, [16]byte{byte(4 + i)}, int32(4+i), nil)
		p.SetInitialDimension(dim)
		p.SetPlayReady(true)
		if !s.Hub.Join(p) {
			t.Fatal("join", dim)
		}
		peers = append(peers, p)
	}
	// Align the first heartbeat with the three-client 1.21.1 trace after
	// unfreezing at age 1. The heartbeat counter also ran while frozen.
	s.serverTicks = 15
	s.World.AdvanceTime(1)
	s.World.SetDayTime(12000)
	var trace strings.Builder
	trace.WriteString("overworld_age,overworld_daytime,nether_age,nether_daytime,end_age,end_daytime\n")
	for i := int64(1); i <= 65; i++ {
		if err := s.TickWorlds(); err != nil {
			t.Fatal(err)
		}
		for index, p := range peers {
			got := drainClockPackets(t, p)
			if (15+i)%20 != 0 {
				if len(got) > 0 {
					t.Fatalf("%s early time sync at %d: %v", p.Dimension(), i, got)
				}
				continue
			}
			if len(got) != 1 {
				t.Fatalf("%s tick %d: got %v, want one heartbeat", p.Dimension(), i, got)
			}
			if index > 0 {
				trace.WriteByte(',')
			}
			fmt.Fprintf(&trace, "%d,%d", got[0][0], got[0][1])
		}
		if (15+i)%20 == 0 {
			trace.WriteByte('\n')
		}
	}
	native, err := os.ReadFile("../../testdata/protocol/world_time_1.21.1.csv")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := trace.String(), strings.ReplaceAll(string(native), "\r\n", "\n"); got != want {
		t.Fatalf("time packets:\n%swant vanilla:\n%s", got, want)
	}
}

func TestScheduledTicksContinueWithDaylightStopped(t *testing.T) {
	s, p, _ := dimensionTestServer(t)
	s.Hub.Leave(p)
	if err := s.World.SetDaylightCycle(false); err != nil {
		t.Fatal(err)
	}
	sand, err := world.BlockStateID("sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := world.ChunkPosition{}
	for _, w := range s.Worlds() {
		if _, err := w.SetChunksForced(c, c, true); err != nil {
			t.Fatal(err)
		}
		if err := w.PrepareDestination(context.Background(), 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := w.SetBlock(8, 220, 8, sand); err != nil {
			t.Fatal(err)
		}
	}
	for tick := 1; tick <= 2; tick++ {
		if err := s.TickWorlds(); err != nil {
			t.Fatal(err)
		}
		want := sand
		if tick == 2 {
			want = world.StateAir
		}
		for _, w := range s.Worlds() {
			if age, day := w.Time(); age != int64(tick) || day != 0 {
				t.Fatal(w.DimensionName(), age, day)
			}
			id, err := w.BlockAt(8, 220, 8)
			if err != nil || id != want {
				t.Fatalf("%s tick %d: sand state %d, want %d (%v)", w.DimensionName(), tick, id, want, err)
			}
		}
	}
}

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
