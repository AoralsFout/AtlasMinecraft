package world

import "errors"

// IgniteTNT replaces the expected block with a full-fuse TNT entity in its
// owning region. A nonzero entity means the edit committed, even if a later
// lighting or neighbor update failed.
func (w *World) IgniteTNT(p BlockPosition, expected uint32, owner int32) (WorldEntity, error) {
	state, err := BlockStateByID(expected)
	if err != nil || state.Name != TNTEntity {
		return WorldEntity{}, ErrBlockChanged
	}
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()

	var entity WorldEntity
	var changed []BlockPosition
	err = w.fallingEntityOperation(p.X>>4, p.Z>>4, func(r *Region, ch *Chunk) error {
		x, z := int(p.X&15), int(p.Z&15)
		if ch.GetBlock(x, z, p.Y) != expected {
			return ErrBlockChanged
		}
		primed, err := w.newPrimedTNT(p, owner, false)
		if err != nil {
			return err
		}
		// Keep both changes in the same actor operation and let the regular
		// chunk save write the terrain and entity snapshots.
		changes := make(chan []BlockPosition, 1)
		err = r.applyBlockEdit(ch, setBlockMsg{x: p.X, y: p.Y, z: p.Z, state: StateAir, deferSave: true, changed: changes})
		changed = <-changes
		if ch.GetBlock(x, z, p.Y) != StateAir {
			return err
		}
		ch.entities = append(ch.entities, primed)
		ch.entitiesDirty = true
		ch.indexLivingDamage()
		entity = primed
		return err
	})
	if entity.Type == "" {
		return entity, err
	}
	const tntPrimedSoundID = 1382 // Java 1.21.1 sound_event registry
	w.playBlockSound(entity.Position, tntPrimedSoundID, 16)
	if len(changed) == 0 {
		changed = []BlockPosition{p}
	}
	return entity, errors.Join(err, w.updateBlockNeighbors(changed))
}
