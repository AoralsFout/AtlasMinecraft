package world

import (
	"errors"
	"testing"
)

func TestAutosaveKeepsSynchronousAndLaterDirtyEdits(t *testing.T) {
	w, _ := mechanicsWorld(t)
	const y, z = 64, 8
	edit := func(x int32, state uint32) {
		t.Helper()
		w.blockUpdateMu.Lock()
		defer w.blockUpdateMu.Unlock()
		if _, err := w.editBlock(setBlockMsg{x: x, y: y, z: z, state: state, raw: true, flags: 2, deferSave: true}); err != nil {
			t.Fatal(err)
		}
	}
	for _, x := range []int32{8, 24} {
		edit(x, StateDirt)
	}

	w.mu.Lock()
	r := w.regions[chunkKey{0, 0}]
	w.mu.Unlock()
	ready := make(chan regionSaveSnapshot, 1)
	r.mailbox <- snapshotRegionSave{result: ready}
	snapshot := <-ready
	if snapshot.err != nil {
		t.Fatal(snapshot.err)
	}
	if err := w.SetBlock(8, y, z, StateStone); err != nil {
		t.Fatal(err)
	}
	edit(24, StateGrass)

	result := snapshot.save.write(w.store.vanilla)
	done := make(chan struct{})
	r.mailbox <- finishRegionSave{save: snapshot.save, writes: result, done: done}
	<-done
	if err := errors.Join(result.entities, result.terrain); err != nil {
		t.Fatal(err)
	}
	check := func(x int32, want uint32) {
		t.Helper()
		ch, err := w.store.vanilla.loadBlockChunk(x>>4, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := ch.GetBlock(int(x&15), z, y); got != want {
			t.Fatalf("saved block %d is %d, want %d", x, got, want)
		}
	}
	check(8, StateStone)
	check(24, StateDirt)
	if err := w.Autosave(); err != nil {
		t.Fatal(err)
	}
	check(8, StateStone)
	check(24, StateGrass)
}
