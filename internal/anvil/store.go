package anvil

import (
	"atlas/internal/nbt"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// Store serializes atomic region replacements with reads of their raw
// records. Different regions can progress independently; readers of the same
// region share a lock. Compression and NBT parsing run outside these locks.
// The zero value is ready to use. Locks are shared with other Store instances
// and the package functions, so all access to one save directory coordinates.
type Store struct{}

var defaultStore Store
var regionLocks [256]sync.RWMutex

func regionLock(dir string, cx, cz int32) *sync.RWMutex {
	path, err := filepath.Abs(dir)
	if err != nil {
		path = filepath.Clean(dir)
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	// A bounded table avoids retaining a lock for every explored region.
	hash := uint32(2166136261)
	for i := 0; i < len(path); i++ {
		hash = (hash ^ uint32(path[i])) * 16777619
	}
	x, z := uint32(cx>>5), uint32(cz>>5)
	return &regionLocks[(hash^x*0x9e3779b9^z*0x85ebca6b)%uint32(len(regionLocks))]
}

func (s *Store) ReadChunk(dir string, cx, cz int32) (string, nbt.Tag, bool, error) {
	lock := regionLock(dir, cx, cz)
	lock.RLock()
	kind, compressed, exists, err := readChunkRecord(dir, cx, cz)
	lock.RUnlock()
	if err != nil || !exists {
		return "", nbt.Tag{}, false, err
	}
	return decodeChunkRecord(kind, compressed, cx, cz)
}

func (s *Store) WriteChunk(dir string, cx, cz int32, name string, root nbt.Tag) error {
	return s.WriteRegionChunks(dir, []ChunkWrite{{X: cx, Z: cz, Name: name, Root: root}})
}

type ChunkWrite struct {
	X, Z int32
	Name string
	Root nbt.Tag

	raw     []byte
	current func() bool
}

// Snapshot serializes the actor-owned NBT before passing a record to a worker.
// current is checked under the region lock immediately before replacement.
func (c ChunkWrite) Snapshot(current func() bool) (ChunkWrite, error) {
	raw, err := nbt.Marshal(c.Name, c.Root)
	if err != nil {
		return ChunkWrite{}, err
	}
	return ChunkWrite{X: c.X, Z: c.Z, raw: raw, current: current}, nil
}

// WriteRegionChunks replaces one region file atomically. All records must be
// distinct and in the same region. Other records are copied without decoding.
func (s *Store) WriteRegionChunks(dir string, chunks []ChunkWrite) error {
	if len(chunks) == 0 {
		return nil
	}
	cx, cz := chunks[0].X, chunks[0].Z
	records := make([]encodedChunk, 0, len(chunks))
	var seen [1024]bool
	for _, chunk := range chunks {
		if chunk.X>>5 != cx>>5 || chunk.Z>>5 != cz>>5 {
			return fmt.Errorf("chunk %d,%d belongs to a different region", chunk.X, chunk.Z)
		}
		index := chunkIndex(chunk.X, chunk.Z)
		if seen[index] {
			return fmt.Errorf("duplicate chunk %d,%d in region write", chunk.X, chunk.Z)
		}
		seen[index] = true
		data := chunk.raw
		var err error
		if data == nil {
			data, err = nbt.Marshal(chunk.Name, chunk.Root)
			if err != nil {
				return fmt.Errorf("encode chunk %d,%d: %w", chunk.X, chunk.Z, err)
			}
		}
		payload, err := compressChunkRecord(data)
		if err != nil {
			return fmt.Errorf("encode chunk %d,%d: %w", chunk.X, chunk.Z, err)
		}
		records = append(records, encodedChunk{index: index, payload: payload, current: chunk.current})
	}
	slices.SortFunc(records, func(a, b encodedChunk) int { return a.index - b.index })
	lock := regionLock(dir, cx, cz)
	lock.Lock()
	defer lock.Unlock()
	records = slices.DeleteFunc(records, func(record encodedChunk) bool {
		if record.current == nil || record.current() {
			return false
		}
		seen[record.index] = false
		return true
	})
	if len(records) == 0 {
		return nil
	}
	return writeChunkRecords(dir, cx, cz, records, seen)
}
