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
	tracking := s.generatedEntities[key]
	if tracking == nil {
		return
	}
	tracking.snapshot = change.Entity
	id := tracking.id
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
