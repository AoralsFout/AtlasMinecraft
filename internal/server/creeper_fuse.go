package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
)

func publishCreeperEffects(players []*player.Player, dimension string, change world.LivingDamageChange) {
	pos := change.Entity.Position
	send := func(id int32, radius float64, payload []byte) {
		for _, p := range players {
			if sink := p.SoundPacketSink(dimension, pos[0], pos[1], pos[2], radius); sink != nil && p.PlayReady() {
				sink.SendPacket(id, payload)
			}
		}
	}
	if silent, _ := change.Entity.Data.Number("Silent"); change.Fuse.Primed && silent == 0 {
		// 767 entity.creeper.primed holder, HOSTILE source, volume 1, pitch 0.5.
		send(proto.SSoundEffect, 16, proto.NewWriter().VarInt(375).VarInt(5).
			I32(int32(pos[0]*8)).I32(int32(pos[1]*8)).I32(int32(pos[2]*8)).
			F32(1).F32(.5).I64(change.SoundSeed).Payload())
	}
	if change.Fuse.Exploded {
		for _, p := range players {
			sink := p.SoundPacketSink(dimension, pos[0], pos[1], pos[2], 64)
			if sink == nil || !p.PlayReady() {
				continue
			}
			impulse := change.Fuse.PlayerImpulse[p.EID]
			// No block edits yet. KEEP, explosion / explosion_emitter, generic.explode.
			sink.SendPacket(proto.SExplosion, proto.NewWriter().F64(pos[0]).F64(pos[1]).F64(pos[2]).
				F32(change.Fuse.Power).VarInt(0).F32(impulse[0]).F32(impulse[1]).F32(impulse[2]).
				VarInt(0).VarInt(22).VarInt(21).VarInt(577).Payload())
		}
	}
}
