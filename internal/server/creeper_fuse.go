package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"math"
)

func publishExplosionEffects(players []*player.Player, dimension string, change world.LivingDamageChange) {
	pos := change.Entity.Position
	send := func(id int32, radius float64, payload []byte) {
		for _, p := range players {
			if sink := p.SoundPacketSink(dimension, pos[0], pos[1], pos[2], radius); sink != nil && p.PlayReady() {
				sink.SendPacket(id, payload)
			}
		}
	}
	if silent, _ := change.Entity.Data.Number("Silent"); change.Fuse != nil && change.Fuse.Primed && silent == 0 {
		// 767 entity.creeper.primed holder, HOSTILE source, volume 1, pitch 0.5.
		send(proto.SSoundEffect, 16, proto.NewWriter().VarInt(375).VarInt(5).
			I32(int32(pos[0]*8)).I32(int32(pos[1]*8)).I32(int32(pos[2]*8)).
			F32(1).F32(.5).I64(change.SoundSeed).Payload())
	}
	if change.Explosion != nil {
		pos := change.Explosion.Center
		for _, p := range players {
			sink := p.SoundPacketSink(dimension, pos[0], pos[1], pos[2], 64)
			if sink == nil || !p.PlayReady() {
				continue
			}
			sink.SendPacket(proto.SExplosion, explosionPacket(change.Explosion, p.EID))
		}
	}
}

func explosionPacket(event *world.ExplosionEvent, playerID int32) []byte {
	pos := event.Center
	w := proto.NewWriter().F64(pos[0]).F64(pos[1]).F64(pos[2]).F32(event.Power).
		VarInt(int32(len(event.Affected)))
	x, y, z := int32(math.Floor(pos[0])), int32(math.Floor(pos[1])), int32(math.Floor(pos[2]))
	for _, cell := range event.Affected {
		w.I8(int8(cell.X - x)).I8(int8(cell.Y - y)).I8(int8(cell.Z - z))
	}
	impulse := event.PlayerImpulse[playerID]
	return w.F32(impulse[0]).F32(impulse[1]).F32(impulse[2]).
		VarInt(event.BlockInteraction).VarInt(22).VarInt(21).VarInt(577).Payload()
}
