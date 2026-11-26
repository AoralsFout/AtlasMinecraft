package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
)

func (s *Server) publishTrapChange(players []*player.Player, dimension string, change world.TrapChange) {
	s.trapRevision.Add(1)
	if len(change.Mounts) > 0 {
		register := s.publishLightningSpawn(players, dimension, change.Bolt)
		register()
	}
	s.entityMu.Lock()
	defer s.entityMu.Unlock()
	cx, cz := fallingChunk(change.Original)
	for _, p := range players {
		sink := p.WorldPacketSinkInDimension(dimension)
		if sink == nil || !p.PlayReady() {
			continue
		}
		if len(change.Mounts) == 0 {
			view := s.generatedViews[p]
			key := generatedEntityKey{dimension, change.Original.UUID}
			if id, visible := view[key]; visible {
				delete(view, key)
				s.releaseGeneratedEntity(key)
				p.MarkInvisible(id)
				sink.SendPacket(proto.SEntityDestroy, destroyPayload([]int32{id}))
			}
			continue
		}
		if !sink.IsChunkSent(cx, cz) || !weatherEntityInRange(p, change.Original) {
			continue
		}
		view := s.generatedEntityView(p)
		for _, mount := range change.Mounts {
			key := generatedEntityKey{dimension, mount.UUID}
			if id, visible := view[key]; visible {
				s.generatedEntities[key].snapshot = mount
				sink.SendPacket(proto.SEntityMetadata, generatedEntityMetadata(id, mount))
			} else {
				s.spawnGeneratedEntity(p, sink, key, mount, view)
			}
			passengers, err := world.EntityPassengers(mount)
			if err != nil {
				s.Logf("trap passenger data: %v", err)
				continue
			}
			var ids []int32
			for _, rider := range passengers {
				if _, ok := generatedEntityTypes[rider.Type]; !ok {
					continue
				}
				child := generatedEntityKey{dimension, rider.UUID}
				if _, visible := view[child]; !visible {
					s.spawnGeneratedEntity(p, sink, child, rider, view)
				}
				ids = append(ids, view[child])
			}
			sink.SendPacket(proto.SSetPassengers, generatedPassengersPayload(view[key], ids))
		}
	}
}
