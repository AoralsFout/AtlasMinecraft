package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
)

// Protocol 767 registry IDs are the positions in the damage_type registry
// sent during configuration. A creeper explosion carries the source entity.
func (s *Server) publishLivingDamage(players []*player.Player, dimension string, change world.LivingDamageChange) {
	if change.Fuse != nil || change.Explosion != nil {
		publishExplosionEffects(players, dimension, change)
	}
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
	if change.Moved && !change.Removed {
		s.publishBlastActorMovement(players, key, change.Entity)
		return
	}
	tracking := s.generatedEntities[key]
	if tracking == nil {
		return
	}
	previous := tracking.snapshot
	if change.Entity.Revision >= previous.Revision {
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
	metadata := livingDamageMetadata(id, previous, change)
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
			var attacker, direct int32
			switch change.Source {
			case "explosion", "player_explosion":
				source = 8
				if change.Source == "player_explosion" {
					source = 33
					attacker = change.SourceEntity + 1
				}
				if change.DirectEntity != 0 {
					direct = change.DirectEntity + 1
				}
			}
			sink.SendPacket(proto.SDamageEvent, proto.NewWriter().VarInt(id).VarInt(source).VarInt(attacker).VarInt(direct).Bool(false).Payload())
		}
		if metadata != nil {
			sink.SendPacket(proto.SEntityMetadata, metadata)
		}
	}
}

// The caller holds entityMu. Blast actors transfer between visible chunks like
// generated entities and publish motion as soon as their entity tick changes it.
func (s *Server) publishBlastActorMovement(players []*player.Player, key generatedEntityKey, entity world.WorldEntity) {
	cx, cz := fallingChunk(entity)
	tracking := s.generatedEntities[key]
	var previous world.WorldEntity
	if tracking != nil {
		previous = tracking.snapshot
		tracking.snapshot = entity
	}
	for _, p := range players {
		sink := p.WorldPacketSinkInDimension(key.dimension)
		if sink == nil || !p.PlayReady() {
			continue
		}
		view := s.generatedViews[p]
		id, visible := view[key]
		if !sink.IsChunkSent(cx, cz) {
			if visible {
				delete(view, key)
				s.releaseGeneratedEntity(key)
				p.MarkInvisible(id)
				sink.SendPacket(proto.SEntityDestroy, destroyPayload([]int32{id}))
			}
			continue
		}
		if !visible {
			s.spawnGeneratedEntity(p, sink, key, entity, s.generatedEntityView(p))
			continue
		}
		var packets []player.OutPacket
		if previous.Position != entity.Position {
			pos, ground := entity.Position, false
			if value, _ := entity.Data.Number("OnGround"); value != 0 {
				ground = true
			}
			teleport := proto.NewWriter().VarInt(id).F64(pos[0]).F64(pos[1]).F64(pos[2]).
				I8(angleI8(entity.Rotation[0])).I8(angleI8(entity.Rotation[1])).Bool(ground).Payload()
			packets = append(packets, player.OutPacket{ID: proto.SEntityTeleport, Payload: teleport})
		}
		velocity := fallingMotion(proto.NewWriter().VarInt(id), entity).Payload()
		packets = append(packets, player.OutPacket{ID: proto.SEntityVelocity, Payload: velocity})
		if entity.Type == world.TNTEntity {
			packets = append(packets, player.OutPacket{ID: proto.SEntityMetadata, Payload: tntFuseMetadata(id, entity)})
		}
		sink.SendPacketsInChunk(cx, cz, packets...)
	}
}

func livingDamageMetadata(id int32, previous world.WorldEntity, change world.LivingDamageChange) []byte {
	if change.Fuse != nil {
		if !change.Fuse.DirectionChanged {
			return nil
		}
		return proto.NewWriter().VarInt(id).U8(16).VarInt(1).VarInt(change.Entity.CreeperSwellDirection()).U8(0xff).Payload()
	}
	hp, _ := change.Entity.Data["Health"].Value.(float32)
	flags := livingFlags(change.Entity)
	w := proto.NewWriter().VarInt(id)
	if change.Entity.Type == "minecraft:creeper" {
		oldHP, _ := previous.Data["Health"].Value.(float32)
		flagsChanged := flags != livingFlags(previous)
		if !flagsChanged && oldHP == hp && !change.CreeperCharged {
			return nil
		}
		if flagsChanged {
			w.U8(0).VarInt(0).U8(flags)
		}
		if oldHP != hp {
			w.U8(9).VarInt(3).F32(hp)
		}
		if change.CreeperCharged {
			w.U8(17).VarInt(8).Bool(true)
		}
	} else {
		w.U8(0).VarInt(0).U8(flags).U8(9).VarInt(3).F32(hp)
	}
	return w.U8(0xff).Payload()
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
