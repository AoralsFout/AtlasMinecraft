package world

import "testing"

func TestCoveredFarmlandResumesAfterRestart(t *testing.T) {
	w, dir := mechanicsWorld(t)
	onlyCenter := func(x, z int32) bool { return x == 0 && z == 0 }
	if err := w.TickScheduled(onlyCenter); err != nil {
		t.Fatal(err)
	}
	field, err := BlockStateID("minecraft:farmland", map[string]string{"moisture": "7"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetBlock(8, 64, 8, field); err != nil {
		t.Fatal(err)
	}
	if err := w.TryPlaceBlock(BlockPosition{8, 65, 8}, StateAir, StateStone, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	ticks := diskBlockTicks(t, dir, 0, 0)
	if len(ticks) != 1 || ticks[0].Type != "minecraft:farmland" || ticks[0].Delay != 1 {
		t.Fatalf("saved ticks %+v", ticks)
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
	if err := reopened.Tick(TickChunks{}); err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.BlockAt(8, 64, 8); err != nil || got != field {
		t.Fatalf("paused farmland %d, err %v", got, err)
	}
	if err := reopened.Tick(TickChunks{Blocks: onlyCenter}); err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.BlockAt(8, 64, 8); err != nil || got != StateDirt {
		t.Fatalf("resumed farmland %d, err %v", got, err)
	}
}
