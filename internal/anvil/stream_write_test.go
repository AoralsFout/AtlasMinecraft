package anvil

import (
	"atlas/internal/nbt"
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestStreamRewritePreservesRawRecordsTimestampsAndPadding(t *testing.T) {
	dir := t.TempDir()
	old := make([]byte, 12*sector)
	for i, kind := range []byte{1, 2, 3, 4, 0x82} {
		offset := 2 + 2*i
		binary.BigEndian.PutUint32(old[i*4:], uint32(offset<<8|2))
		binary.BigEndian.PutUint32(old[sector+i*4:], uint32(12345+i))
		raw := old[offset*sector : (offset+2)*sector]
		for j := range raw {
			raw[j] = byte(j*31 + i)
		}
		binary.BigEndian.PutUint32(raw, 50)
		raw[4] = kind
	}
	path := regionPath(dir, 0, 0)
	if err := os.WriteFile(path, old, 0644); err != nil {
		t.Fatal(err)
	}
	root := func(x int32) nbt.Tag {
		return nbt.Tag{Type: nbt.Compound, Value: nbt.Object{"xPos": {Type: nbt.Int, Value: x}}}
	}
	err := defaultStore.WriteRegionChunks(dir, []ChunkWrite{
		{X: 5, Root: root(5)}, {X: 2, Root: root(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if i == 2 {
			continue
		}
		loc := binary.BigEndian.Uint32(got[i*4:])
		offset, count := int(loc>>8), int(loc&255)
		if count != 2 || !bytes.Equal(old[(2+2*i)*sector:(4+2*i)*sector], got[offset*sector:(offset+count)*sector]) {
			t.Fatalf("raw record/padding changed: %d", i)
		}
		if !bytes.Equal(old[sector+i*4:sector+i*4+4], got[sector+i*4:sector+i*4+4]) {
			t.Fatalf("neighbor timestamp changed: %d", i)
		}
	}
	for _, x := range []int32{2, 5} {
		_, got, exists, err := ReadChunk(dir, x, 0)
		object, _ := got.Object()
		value, _ := object.Number("xPos")
		if err != nil || !exists || value != int64(x) {
			t.Fatalf("replacement %d not readable: exists=%t xPos=%d err=%v", x, exists, value, err)
		}
	}
}

func TestRegionBatchValidationLeavesOriginalUntouched(t *testing.T) {
	dir := t.TempDir()
	root := nbt.Tag{Type: nbt.Compound, Value: nbt.Object{}}
	if err := WriteChunk(dir, -2, 0, "original", root); err != nil {
		t.Fatal(err)
	}
	path := regionPath(dir, -2, 0)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, second := range []ChunkWrite{
		{X: -2, Root: root}, // duplicate
		{X: 30, Root: root}, // same local slot in a different region
		{X: -1, Root: nbt.Tag{Type: nbt.String, Value: "invalid root"}},
	} {
		if err := defaultStore.WriteRegionChunks(dir, []ChunkWrite{{X: -2, Root: root}, second}); err == nil {
			t.Fatalf("invalid batch accepted: %+v", second)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("invalid batch modified the region: %v", err)
		}
	}
}

func TestStreamRewriteInvalidNeighborLeavesOriginalUntouched(t *testing.T) {
	dir := t.TempDir()
	old := make([]byte, 3*sector)
	binary.BigEndian.PutUint32(old[4:], uint32(100<<8|1))
	path := regionPath(dir, 0, 0)
	if err := os.WriteFile(path, old, 0644); err != nil {
		t.Fatal(err)
	}
	root := nbt.Tag{Type: nbt.Compound, Value: nbt.Object{}}
	if err := WriteChunk(dir, 0, 0, "", root); err == nil {
		t.Fatal("invalid neighbor was silently dropped")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(old, got) {
		t.Fatal("failed replacement changed original")
	}
}
