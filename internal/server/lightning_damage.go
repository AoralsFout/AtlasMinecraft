package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
)

// Protocol 767 registry IDs are the positions in the damage_type registry
// sent during configuration. Neither environmental source has an attacker.
func (s *Server) publishLivingDamage(players []*player.Player, dimension string, change world.LivingDamageChange) {
	key := generatedEntityKey{dimension, change.Entity.UUID}
	s.trapRevision.Add(1)
	s.entityMu.Lock()
	defer s.entityMu.Unlock()
	if change.Spawned {
		cx, cz := fallingChunk(change.Entity)
		for _, p := range players {
			sink := p.WorldPacketSinkInDimension(dimension)
			if sink == nil || !p.PlayReady() || !sink.IsChunkSent(cx, cz) {
				continue
			}
			view := s.generatedEntityView(p)
			if _, seen := view[key]; !seen {
				s.spawnGeneratedEntity(p, sink, key, change.Entity, view)
			}
		}
		return
	}
	tracking := s.generatedEntities[key]
	if tracking == nil {
		return
	}
	if change.Entity.Revision >= tracking.snapshot.Revision {
		tracking.snapshot = change.Entity
	}
	id := tracking.id
	if change.MooshroomVariant != "" {
		// Red is the spawn default but must still be sent on a brown -> red change.
		packet := proto.NewWriter().VarInt(id).U8(17).VarInt(4).String(change.MooshroomVariant).U8(0xff).Payload()
		for _, p := range players {
			if _, visible := s.generatedViews[p][key]; !visible {
				continue
			}
			if sink := p.WorldPacketSinkInDimension(dimension); sink != nil {
				sink.SendPacket(proto.SEntityMetadata, packet)
			}
		}
		return
	}
	hp, _ := change.Entity.Data["Health"].Value.(float32)
	flags := livingFlags(change.Entity)
	metadata := proto.NewWriter().VarInt(id).U8(0).VarInt(0).U8(flags).U8(9).VarInt(3).F32(hp).U8(0xff).Payload()
	for _, p := range players {
		view := s.generatedViews[p]
		if _, visible := view[key]; !visible {
			continue
		}
		sink := p.WorldPacketSinkInDimension(dimension)
		if sink == nil {
			continue
		}
		if change.Removed {
			delete(view, key)
			s.releaseGeneratedEntity(key)
			p.MarkInvisible(id)
			sink.SendPacket(proto.SEntityDestroy, destroyPayload([]int32{id}))
			continue
		}
		if change.Source != "" {
			source := int32(24)
			if change.Source == "on_fire" {
				source = 29
			}
			sink.SendPacket(proto.SDamageEvent, proto.NewWriter().VarInt(id).VarInt(source).VarInt(0).VarInt(0).Bool(false).Payload())
		}
		sink.SendPacket(proto.SEntityMetadata, metadata)
	}
}

func livingFlags(e world.WorldEntity) byte {
	var flags byte
	if e.OnFire {
		flags |= 1
	}
	if invisible, _ := e.Data.Number("Invisible"); invisible != 0 {
		flags |= 0x20
	}
	if glowing, _ := e.Data.Number("Glowing"); glowing != 0 {
		flags |= 0x40
	}
	return flags
}

func publishMooshroomSound(players []*player.Player, dimension string, change world.LivingDamageChange) {
	if silent, _ := change.Entity.Data.Number("Silent"); silent != 0 {
		return
	}
	pos := change.Entity.Position
	// Registered holder (+1), NEUTRAL source, volume 2 and pitch 1.
	packet := proto.NewWriter().VarInt(826 + 1).VarInt(6).I32(int32(pos[0] * 8)).I32(int32(pos[1] * 8)).I32(int32(pos[2] * 8)).F32(2).F32(1).I64(change.SoundSeed).Payload()
	for _, p := range players {
		if sink := p.SoundPacketSink(dimension, pos[0], pos[1], pos[2], 32); p.PlayReady() && sink != nil {
			sink.SendPacket(proto.SSoundEffect, packet)
		}
	}
}
