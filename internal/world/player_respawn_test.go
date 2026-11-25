package world

import (
	"atlas/internal/nbt"
	"context"
	"errors"
	"testing"
)

func TestBedRespawnYawAgainstVanilla(t *testing.T) {
	for offset, want := range map[BlockPosition]float32{
		{1, 0, 1}: 135, {-1, 0, 1}: -134.99998, {-1, 0, -2}: -26.564848, {}: -90, {1, -1, 2}: 153.43495,
	} {
		if got := respawnYaw(offset, 0); got != want {
			t.Errorf("%v yaw %v, want %v", offset, got, want)
		}
	}
}

func TestEndReturnPositionsAgainstVanilla(t *testing.T) {
	w, dir := mechanicsWorld(t)
	nether, err := OpenDimension(noViewers{}, 1, dir, "the_nether")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := nether.Close(); err != nil {
			t.Error(err)
		}
	})
	set := func(w *World, p BlockPosition, name string, props map[string]string) {
		t.Helper()
		id, err := BlockStateID(name, props)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.SetBlock(p.X, p.Y, p.Z, id); err != nil {
			t.Fatal(err)
		}
	}
	point := PersonalSpawn{BlockPosition: BlockPosition{-1592, 70, -1592}, Angle: 37, Forced: true}
	set(w, point.BlockPosition, "stone", nil)
	if _, _, found, err := w.ResolvePersonalSpawn(point, false); err != nil || found {
		t.Fatal("obstructed forced spawn accepted", err)
	}
	if _, err := w.SetSpawn(-1592, 75, -1592, 44); err != nil {
		t.Fatal(err)
	}
	v := w.store.vanilla
	v.mu.Lock()
	root, _ := v.root.Object()
	data, _ := root.Object("Data")
	rules, _ := data.Object("GameRules")
	rules["spawnRadius"] = nbt.Tag{Type: nbt.String, Value: "0"}
	v.mu.Unlock()
	fallback, err := w.SharedRespawn(context.Background())
	if err != nil || fallback.X != -1591.5 || fallback.Y != 71 || fallback.Z != -1591.5 || fallback.Yaw != 0 {
		t.Fatal("shared return", fallback, err)
	}
	point.Y = 67
	set(w, BlockPosition{-1591, 66, -1592}, "stone", nil)
	set(w, point.BlockPosition, "red_bed", map[string]string{"facing": "north", "occupied": "false", "part": "head"})
	bed, spend, ok, err := w.ResolvePersonalSpawn(point, false)
	if err != nil || !ok || spend != nil || bed.X != -1590.5 || bed.Y != 67 || bed.Z != -1591.5 || bed.Yaw != float32(89.99999237060547) {
		t.Fatal("bed return", bed, ok, err)
	}

	point = PersonalSpawn{BlockPosition: BlockPosition{160, 240, 160}, Angle: 37}
	set(nether, BlockPosition{160, 239, 159}, "stone", nil)
	set(nether, point.BlockPosition, "respawn_anchor", map[string]string{"charges": "2"})
	for _, first := range []bool{true, false} {
		pos, spend, ok, err := nether.ResolvePersonalSpawn(point, first)
		if err != nil || !ok || pos.X != 160.5 || pos.Y != 240 || pos.Z != 159.5 || pos.Yaw != float32(-4.9415598368796054e-06) {
			t.Fatal("anchor return", pos, ok, err)
		}
		if first && spend != nil || !first && spend == nil {
			t.Fatalf("unexpected charge edit, first=%v", first)
		}
		if spend != nil {
			if err := spend(); err != nil {
				t.Fatal(err)
			}
			if err := spend(); !errors.Is(err, ErrBlockChanged) {
				t.Fatal("stale anchor charge accepted", err)
			}
		}
	}
	id, err := nether.BlockAt(160, 240, 160)
	want, _ := BlockStateID("respawn_anchor", map[string]string{"charges": "1"})
	if err != nil || id != want {
		t.Fatal("anchor charge", id, err)
	}
}
