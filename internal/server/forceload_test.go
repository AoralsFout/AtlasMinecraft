package server

import (
	"atlas/internal/hub"
	"atlas/internal/player"
	"atlas/internal/world"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Use the command and server clock entry points. Keep the wheat in daylight
// and irrigated so that absence of a random tick is its only simulation gate.
func TestForceLoadWorldClockAndRestart(t *testing.T) {
	dir := t.TempDir()
	seed := int64(1234)
	if err := world.CreateFlatWorld(dir, &seed); err != nil {
		t.Fatal(err)
	}
	open := func() *Server {
		h := hub.New()
		w, err := world.Open(hub.DimensionView{Hub: h, Dimension: "minecraft:overworld"}, 2, dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		})
		return New(Config{SimulationDistance: 1, ViewDistance: 2}, h, w)
	}
	s := open()
	p := player.New("Builder", [16]byte{1}, 1, nil)
	p.SetPos(-1592, 64, 1608, 0, 0, true)
	p.SetPlayReady(true)
	if !s.Hub.Join(p) {
		t.Fatal("join")
	}
	command := func(line, want string) {
		t.Helper()
		if got := s.executeCommand(nil, line); !strings.Contains(got, want) {
			t.Fatalf("%s: %s, want %s", line, got, want)
		}
	}
	tick := func(n int) {
		t.Helper()
		for range n {
			if err := s.TickWorlds(); err != nil {
				t.Fatal(err)
			}
		}
	}
	command("gamerule spawnChunkRadius 0", "now set to: 0")
	command("forceload add -1600 1600", "Marked 1")
	tick(1)
	if got := s.executeCommand(p, "forceload remove all"); !strings.Contains(got, "permission") {
		t.Fatal(got)
	}
	// Wait for the asynchronous FORCED ticket without manually loading its neighbors.
	ready := func() {
		var points []world.BlockPosition
		for x := int32(-102); x <= -98; x++ {
			for z := int32(98); z <= 102; z++ {
				points = append(points, world.BlockPosition{X: x * 16, Y: 63, Z: z * 16})
			}
		}
		deadline := time.Now().Add(time.Minute)
		for {
			_, err := s.World.ReadLoadedBlocks(points)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("forced neighborhood unavailable", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	ready()
	blockID := func(name string, props map[string]string) uint32 {
		id, err := world.BlockStateID(name, props)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	put := func(x, y, z int32, id uint32) {
		t.Helper()
		if err := s.World.SetBlock(x, y, z, id); err != nil {
			t.Fatal(err)
		}
	}
	check := func(x, y, z int32, id uint32) {
		t.Helper()
		got, err := s.World.ReadLoadedBlocks([]world.BlockPosition{{X: x, Y: y, Z: z}})
		if err != nil || got[0] != id {
			t.Fatalf("block %d,%d,%d: %v want %d (%v)", x, y, z, got, id, err)
		}
	}
	const x, z int32 = -1593, 1608
	water := blockID("water", nil)
	wheat := blockID("wheat", nil)
	for _, dx := range []int32{-1, 0, 1} {
		for _, dz := range []int32{-1, 0, 1} {
			put(x+dx, 63, z+dz, world.StateStone)
			if dx != 0 || dz != 0 {
				put(x+dx, 64, z+dz, world.StateStone)
			}
		}
	}
	put(x, 64, z, water)
	put(x+2, 64, z, blockID("farmland", map[string]string{"moisture": "7"}))
	put(x+2, 65, z, wheat)
	put(x+1, 64, z, world.StateAir)
	if !s.World.RandomTickEligible(-100, 100) {
		t.Fatal("nearby player did not enable random ticks")
	}
	s.Hub.Leave(p)
	tick(2)
	if s.World.RandomTickEligible(-100, 100) {
		t.Fatal("force loading alone enabled random ticks")
	}
	check(x+1, 64, z, world.StateAir)
	if err := s.World.Close(); err != nil {
		t.Fatal(err)
	}
	s = open()
	command("forceload query -1600 1600", "is marked")
	ready()
	// Three of water's five ticks remain at restart.
	tick(2)
	check(x+1, 64, z, world.StateAir)
	tick(1)
	check(x+1, 64, z, blockID("water", map[string]string{"level": "1"}))
	check(x+2, 65, z, wheat)
	if s.World.RandomTickEligible(-100, 100) {
		t.Fatal("reopened forced chunk received random ticks")
	}
	// Open a second outlet. Removing the last ticket must pause the scheduled fluid.
	put(x-1, 64, z, world.StateAir)
	command("forceload remove -1600 1600", "Unmarked 1")
	tick(6)
	check(x-1, 64, z, world.StateAir)
	if n, err := s.World.UnloadIdle(0); err != nil || n != 25 {
		t.Fatalf("removed forced chunks: unloaded %d, err %v", n, err)
	}
	if err := s.World.Close(); err != nil {
		t.Fatal(err)
	}
	s = open()
	command("forceload query", "No force loaded")
	tick(1)
	if n := s.World.Stats().LoadedChunks; n != 0 {
		t.Fatalf("restored %d removed chunks", n)
	}
}

func TestForceLoadCommandsAndDimensionFiles(t *testing.T) {
	s, p, dir := dimensionTestServer(t)
	cmd := func(source *player.Player, line, want string) {
		t.Helper()
		if got := s.executeCommand(source, line); !strings.Contains(got, want) {
			t.Fatalf("%s: %s want %s", line, got, want)
		}
	}
	cmd(nil, "forceload add 0 0 4096 0", "256")
	cmd(nil, "forceload add 30000000 0", "Invalid")
	cmd(nil, "forceload add ^ ^", "Invalid")
	cmd(nil, "forceload query", "No force loaded")
	// Negative fractional relative coordinates floor before conversion to chunk positions.
	cmd(p, "minecraft:forceload add ~-1 ~ 0 0", "Marked 2")
	cmd(nil, "forceload add 0 0", "No forced chunks changed")
	cmd(nil, "forceload query", "[-1, 0], [0, 0]")
	cmd(nil, "forceload remove all", "Unmarked 2")
	// A command in the End must leave the Overworld set empty.
	cmd(nil, "execute in minecraft:the_end run forceload add 0 0", "Marked 1")
	cmd(nil, "forceload query 0 0", "not marked")
	cmd(nil, "execute in minecraft:the_end run forceload query 0 0", "is marked")
	if _, err := os.Stat(filepath.Join(dir, "DIM1", "data", "chunks.dat")); err != nil {
		t.Fatal(err)
	}
	cmd(nil, "execute in minecraft:the_end run forceload remove all", "Unmarked 1")
	nodes := readCommandTree(t, declareCommandsPayloadForLevel(2))
	if n, ok := treePath(nodes, "forceload", "add", "from", "to"); !ok || n.parser != 9 || !n.executable {
		t.Fatal("missing column arguments")
	}
}
