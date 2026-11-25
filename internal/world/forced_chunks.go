package world

import (
	"atlas/internal/javasave"
	"atlas/internal/nbt"
	"atlas/internal/tickqueue"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
)

// ForcedChunksSavedData stores only the centers, in the owning dimension.
type forcedChunks struct {
	positions map[ChunkPosition]struct{}
	name      string
	root      nbt.Object
}

func forcedChunkPath(v *VanillaSave) string {
	return filepath.Join(v.dir, v.dimension.regionSubdir, "data", "chunks.dat")
}

func readForcedChunks(v *VanillaSave) (forcedChunks, error) {
	var forced forcedChunks
	path := forcedChunkPath(v)
	b, err := javasave.ReadGzip(path)
	if errors.Is(err, os.ErrNotExist) {
		return forced, nil
	}
	if err != nil {
		return forced, err
	}
	name, tag, err := nbt.Parse(b)
	if err != nil {
		return forced, fmt.Errorf("read %s: %w", path, err)
	}
	root, _ := tag.Object()
	data, _ := root.Object("data")
	values, ok := data["Forced"].Longs()
	if !ok {
		return forced, fmt.Errorf("%s: missing data.Forced long array", path)
	}
	forced = forcedChunks{positions: make(map[ChunkPosition]struct{}, len(values)), name: name, root: root}
	for _, v := range values {
		pos := ChunkPosition{int32(v), int32(v >> 32)}
		if !validForcedCenter(pos) {
			return forced, fmt.Errorf("%s: forced chunk outside world: %v", path, pos)
		}
		forced.positions[pos] = struct{}{}
	}
	return forced, nil
}

func validForcedCenter(p ChunkPosition) bool {
	return p.X >= -worldChunkLimit && p.X < worldChunkLimit && p.Z >= -worldChunkLimit && p.Z < worldChunkLimit
}

// Persist the complete command before publishing new tickets. Failure leaves
// both the previous set and the live simulation in effect.
func (w *World) commitForcedChunks(next map[ChunkPosition]struct{}) error {
	w.saveMu.Lock()
	defer w.saveMu.Unlock()
	select {
	case <-w.done:
		return ErrWorldClosed
	default:
	}
	if w.store != nil {
		root := maps.Clone(w.forced.root)
		if root == nil {
			root = nbt.Object{"DataVersion": {Type: nbt.Int, Value: int32(3955)}}
		}
		old, _ := root.Object("data")
		data := maps.Clone(old)
		if data == nil {
			data = nbt.Object{}
		}
		values := make([]int64, 0, len(next))
		for p := range next {
			values = append(values, int64(tickqueue.ChunkKey(p.X, p.Z)))
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		data["Forced"] = nbt.Tag{Type: nbt.LongArray, Value: values}
		root["data"] = nbt.Tag{Type: nbt.Compound, Value: data}
		path := forcedChunkPath(w.store.vanilla)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := javasave.WriteNBT(path, w.forced.name, nbt.Tag{Type: nbt.Compound, Value: root}); err != nil {
			return err
		}
		w.forced.root = root
	}
	w.forced.positions = next
	w.rebuildSimulationTicketsLocked()
	return nil
}

// SetChunksForced applies one inclusive /forceload rectangle in chunk coordinates.
func (w *World) SetChunksForced(from, to ChunkPosition, force bool) (int, error) {
	if !validForcedCenter(from) || !validForcedCenter(to) {
		return 0, fmt.Errorf("forced chunk outside world")
	}
	x0, x1 := min(from.X, to.X), max(from.X, to.X)
	z0, z1 := min(from.Z, to.Z), max(from.Z, to.Z)
	if int64(x1-x0+1)*int64(z1-z0+1) > 256 {
		return 0, fmt.Errorf("a forceload range cannot exceed 256 chunks")
	}
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	next := maps.Clone(w.forced.positions)
	if next == nil {
		next = make(map[ChunkPosition]struct{})
	}
	changed := 0
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			p := ChunkPosition{x, z}
			if _, ok := next[p]; ok == force {
				continue
			}
			if force {
				next[p] = struct{}{}
			} else {
				delete(next, p)
			}
			changed++
		}
	}
	if changed == 0 {
		return 0, nil
	}
	if err := w.commitForcedChunks(next); err != nil {
		return 0, err
	}
	return changed, nil
}

func (w *World) ClearForcedChunks() (int, error) {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	n := len(w.forced.positions)
	if n == 0 {
		return 0, nil
	}
	if err := w.commitForcedChunks(nil); err != nil {
		return 0, err
	}
	return n, nil
}

func (w *World) ForcedChunks() []ChunkPosition {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	out := make([]ChunkPosition, 0, len(w.forced.positions))
	for p := range w.forced.positions {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].X < out[j].X || out[i].X == out[j].X && out[i].Z < out[j].Z })
	return out
}
