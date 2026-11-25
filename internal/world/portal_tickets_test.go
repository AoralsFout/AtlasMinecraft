package world

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPortalTicketRenewalAndExpiryPhases(t *testing.T) {
	w, _ := mechanicsWorld(t)
	if err := w.SetSpawnChunkRadius(0); err != nil {
		t.Fatal(err)
	}
	center := ChunkPosition{0, 0}
	if _, err := w.SetChunksForced(center, center, true); err != nil {
		t.Fatal(err)
	}
	w.PlacePortalTicket(BlockPosition{1, 80, 1})
	tick := func(n int) {
		t.Helper()
		for range n {
			if err := w.TickPlayers(nil, 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	tick(102)
	snap := w.simulation.Load()
	w.PlacePortalTicket(BlockPosition{1, 80, 1})
	if w.simulation.Load() != snap {
		t.Fatal("renewal rebuilt an unchanged ticket area")
	}
	tick(298)
	if w.RandomTickEligible(0, 0) {
		t.Fatal("portal ticket supplied player proximity")
	}
	deadline := time.Now().Add(time.Minute)
	for w.Stats().LoadedChunks != 49 {
		if time.Now().After(deadline) {
			t.Fatalf("portal loaded %d chunks, want 49", w.Stats().LoadedChunks)
		}
		time.Sleep(5 * time.Millisecond)
	}
	sand, err := BlockStateID("sand", nil)
	if err != nil {
		t.Fatal(err)
	}
	place := func(x int32) {
		t.Helper()
		if err := w.SetBlock(x, 80, 8, sand); err != nil {
			t.Fatal(err)
		}
	}
	check := func(x int32, want uint32) {
		t.Helper()
		got, err := w.BlockAt(x, 80, 8)
		if err != nil || got != want {
			t.Fatalf("block x=%d got %d, want %d: %v", x, got, want, err)
		}
	}
	var report strings.Builder
	report.WriteString("age,center_level,entity_ticking,block_ticking,full\n")
	record := func(age int) {
		t.Helper()
		current := w.simulation.Load()
		entity, block, full := 0, 0, 0
		for _, ticket := range current.chunks {
			switch {
			case ticket.level <= 31:
				entity++
			case ticket.level == 32:
				block++
			case ticket.level == 33:
				full++
			default:
				t.Fatalf("unexpected active chunk level %d", ticket.level)
			}
		}
		fmt.Fprintf(&report, "%d,%d,%d,%d,%d\n", age, current.chunks[chunkKey{0, 0}].level, entity, block, full)
	}
	tick(1)
	record(299)
	place(24)
	place(40)
	tick(1)
	record(300)
	check(40, sand)
	tick(1)
	record(301)
	// Both blocks ran before purge. The new falling entity in ring one
	// no longer has entity eligibility after the downgrade to FORCED.
	check(24, StateAir)
	check(40, StateAir)
	falling, err := w.EntitiesInChunk(1, 0)
	if err != nil || len(falling) != 1 {
		t.Fatalf("ring-one falling sand: %d entities, %v", len(falling), err)
	}
	if age, ok := falling[0].Data.Number("Time"); !ok || age != 0 {
		t.Fatalf("entity tick used expired portal ticket: age %d", age)
	}
	native, err := os.ReadFile("../../testdata/worldgen/portal_ticket_expiry_1.21.1.csv")
	if err != nil {
		t.Fatal(err)
	}
	if report.String() != strings.ReplaceAll(string(native), "\r\n", "\n") {
		t.Fatalf("ticket expiry report:\n%swant original 1.21.1:\n%s", report.String(), native)
	}
	place(41)
	tick(2)
	check(41, sand)
}

func TestPortalTicketOverlapAndRestart(t *testing.T) {
	w, dir := mechanicsWorld(t)
	players := []SimulationPlayer{{X: -8, Z: 8, Spectator: true}}
	tick := func(n int) {
		t.Helper()
		for range n {
			if err := w.TickPlayers(players, 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The first destination overlaps both the player and default spawn.
	w.PlacePortalTicket(BlockPosition{-1, 80, 0})
	tick(100)
	w.PlacePortalTicket(BlockPosition{-48, 80, 0})
	tick(201)
	snap := w.simulation.Load()
	if snap.chunks[chunkKey{0, 0}].level != 30 || snap.chunks[chunkKey{-3, 0}].level != 30 || len(w.portalTicks) != 1 {
		t.Fatal("expiring one portal changed another source")
	}
	if err := w.SetSpawnChunkRadius(0); err != nil {
		t.Fatal(err)
	}
	tick(100)
	if level := w.simulation.Load().chunks[chunkKey{-1, 0}].level; level != 31 {
		t.Fatalf("lost player ticket: level %d", level)
	}
	players = nil
	w.PlacePortalTicket(BlockPosition{1, 80, 1})
	tick(301)
	if w.simulation.Load() != nil {
		t.Fatal("portal alone still held chunks after age 301")
	}
	w.PlacePortalTicket(BlockPosition{1, 80, 1})
	if err := w.Close(); err != nil {
		t.Fatal(err)
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
	if err := reopened.TickPlayers(nil, 1); err != nil || reopened.simulation.Load() != nil {
		t.Fatalf("restarted world retained temporary ticket: %v", err)
	}
}
