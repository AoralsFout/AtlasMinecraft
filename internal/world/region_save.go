package world

import (
	"atlas/internal/anvil"
	"atlas/internal/light"
	"errors"
	"fmt"
	"sync/atomic"
)

type snapshotRegionSave struct{ result chan regionSaveSnapshot }

type regionSaveSnapshot struct {
	save regionSave
	err  error
}

type finishRegionSave struct {
	save   regionSave
	writes regionSaveResult
	done   chan struct{}
}

type regionSave struct {
	entities, terrain regionSaveGroup
}

type regionSaveGroup struct {
	records []anvil.ChunkWrite
	chunks  []regionSaveChunk
}

type regionSaveChunk struct {
	chunk        *Chunk
	epoch        uint64
	tickRevision uint64
	tickTime     int64
}

type regionSaveResult struct {
	entities, terrain error
}

func (r *Region) saveChunks() error {
	if r.world.store == nil {
		return nil
	}
	save, err := r.collectChunkSaves(false)
	writes := save.write(r.world.store.vanilla)
	r.finishChunkSaves(save, writes, false)
	return errors.Join(err, writes.entities, writes.terrain)
}

func (r *Region) collectChunkSaves(background bool) (regionSave, error) {
	v := r.world.store.vanilla
	var save regionSave
	var combined error
	for key, ch := range r.chunks {
		if ch.entitiesDirty || !background && ch.entitySavePending {
			record, err := v.entityChunkWrite(ch, ch.entities)
			if err == nil {
				err = save.entities.add(ch, record, &ch.entitySaveEpoch, background)
			}
			if err != nil {
				combined = errors.Join(combined, fmt.Errorf("snapshot entities %d,%d: %w", key.x, key.z, err))
			} else if background {
				ch.entitySavePending = true
				ch.entitiesDirty = false
			}
		}
		if ch.scheduledTicks != nil && ch.scheduledTicks.needsSave() {
			ch.dirty = true
		}
		v.refreshStrongholdFlags(ch)
		if r.world.lighting != nil {
			r.applyLight(ch, r.world.lighting.Snapshot(light.Position{X: key.x, Z: key.z}), false)
		}
		if !ch.dirty && (background || !ch.terrainSavePending) {
			continue
		}
		name, root, err := ch.updateAnvilBlocks()
		if err == nil {
			record := anvil.ChunkWrite{X: key.x, Z: key.z, Name: name, Root: root}
			err = save.terrain.add(ch, record, &ch.terrainSaveEpoch, background)
		}
		if err != nil {
			combined = errors.Join(combined, fmt.Errorf("snapshot terrain %d,%d: %w", key.x, key.z, err))
		} else if background {
			// updateAnvilBlocks has carried the dirty sections into the actor's
			// retained NBT. Later edits get fresh dirty flags. On failure the
			// retained NBT is still available for the retry.
			ch.terrainSavePending = true
			ch.dirty = false
			clear(ch.dirtySections)
		}
	}
	return save, combined
}

func (g *regionSaveGroup) add(ch *Chunk, record anvil.ChunkWrite, epoch *atomic.Uint64, background bool) error {
	captured := epoch.Load()
	if background {
		var err error
		record, err = record.Snapshot(func() bool { return epoch.Load() == captured })
		if err != nil {
			return err
		}
	} else {
		// A synchronous save must win over any outstanding periodic snapshot.
		epoch.Add(1)
	}
	g.records = append(g.records, record)
	g.chunks = append(g.chunks, regionSaveChunk{
		chunk: ch, epoch: captured, tickRevision: ch.tickSaveRevision, tickTime: ch.tickSaveTime,
	})
	return nil
}

func (s regionSave) write(v *VanillaSave) regionSaveResult {
	var result regionSaveResult
	if err := v.regions.WriteRegionChunks(v.entityDirectory(), s.entities.records); err != nil {
		result.entities = fmt.Errorf("save entity region: %w", err)
	}
	if err := v.regions.WriteRegionChunks(v.regionDir, s.terrain.records); err != nil {
		result.terrain = fmt.Errorf("save terrain region: %w", err)
	}
	return result
}

func (r *Region) finishChunkSaves(save regionSave, result regionSaveResult, background bool) {
	for _, saved := range save.entities.chunks {
		ch := saved.chunk
		if background {
			if r.chunks[chunkKey{ch.cx, ch.cz}] != ch {
				continue
			}
			ch.entitySavePending = false
			if result.entities != nil || saved.epoch != ch.entitySaveEpoch.Load() {
				ch.entitiesDirty = true
			}
		} else if result.entities == nil {
			r.markEntitiesSaved(ch)
		}
	}
	for _, saved := range save.terrain.chunks {
		ch := saved.chunk
		if background {
			if r.chunks[chunkKey{ch.cx, ch.cz}] != ch {
				continue
			}
			ch.terrainSavePending = false
			if result.terrain != nil || saved.epoch != ch.terrainSaveEpoch.Load() {
				ch.dirty = true
			} else if ch.scheduledTicks != nil {
				ch.scheduledTicks.saved(saved.tickRevision, saved.tickTime)
			}
		} else if result.terrain == nil {
			ch.markAnvilSaved()
		}
	}
}
