package server

import (
	"atlas/internal/player"
	"atlas/internal/proto"
	"atlas/internal/world"
	"math"
)

// These IDs and the entity packets are checked against the actual 1.21.1
// registry and codecs in GeneratedEntityPacketProbe. Unknown saved types are
// retained by world storage until their runtime behavior is implemented.
var generatedEntityTypes = map[string]int32{
	"minecraft:end_crystal":     30,
	"minecraft:witch":           118,
	"minecraft:cat":             15,
	"minecraft:villager":        113,
	"minecraft:zombie_villager": 126,
	"minecraft:sheep":           87,
	"minecraft:pig":             77,
	"minecraft:chicken":         19,
	"minecraft:cow":             22,
	"minecraft:creeper":         23,
	"minecraft:horse":           53,
	"minecraft:skeleton":        91,
	world.SkeletonHorseEntity:   92,
	"minecraft:donkey":          25,
	"minecraft:armadillo":       2,
	"minecraft:fox":             42,
	"minecraft:frog":            43,
	"minecraft:goat":            49,
	"minecraft:llama":           65,
	"minecraft:mooshroom":       70,
	"minecraft:panda":           74,
	"minecraft:parrot":          75,
	"minecraft:polar_bear":      81,
	"minecraft:rabbit":          84,
	"minecraft:turtle":          111,
	"minecraft:wolf":            122,
	"minecraft:piglin":          78,
	"minecraft:piglin_brute":    79,
	"minecraft:hoglin":          51,
	"minecraft:drowned":         27,
	"minecraft:allay":           0,
	"minecraft:iron_golem":      57,
	"minecraft:chest_minecart":  18,
	"minecraft:shulker":         88,
	"minecraft:item_frame":      60,
	"minecraft:armor_stand":     3,
	"minecraft:camel":           14,

	"minecraft:zombified_piglin": 127,
}

type generatedEntityKey struct {
	dimension string
	uuid      [16]byte
}

type generatedEntityTracking struct {
	id       int32
	viewers  int
	snapshot world.WorldEntity // latest immutable snapshot, protected by entityMu
}

func generatedEntitySpawnPayload(id int32, entity world.WorldEntity) []byte {
	angle := func(value float32) int8 { return int8(int32(math.Floor(float64(value * 256 / 360)))) }
	position, data := entity.Position, int32(0)
	headYaw := int8(int32(math.Floor(float64(entity.Rotation[0]) * 256 / 360)))
	if entity.Type == "minecraft:item_frame" {
		for i, key := range []string{"TileX", "TileY", "TileZ"} {
			if value, ok := entity.Data.Number(key); ok {
				position[i] = float64(value)
			}
		}
		facing, _ := entity.Data.Number("Facing")
		data = int32(facing)
		headYaw = 0
	}
	w := proto.NewWriter().VarInt(id).UUID(entity.UUID).VarInt(generatedEntityTypes[entity.Type]).
		F64(position[0]).F64(position[1]).F64(position[2]).
		I8(angle(entity.Rotation[1])).I8(angle(entity.Rotation[0])).I8(headYaw).
		VarInt(data)
	if entity.Type == world.SkeletonHorseEntity {
		return fallingMotion(w, entity).Payload()
	}
	return w.I16(0).I16(0).I16(0).Payload()
}

func generatedEntityMetadata(id int32, entity world.WorldEntity) []byte {
	if entity.Type == "minecraft:armor_stand" {
		return generatedArmorStandMetadata(id, entity)
	}
	if entity.Type == "minecraft:item_frame" {
		return generatedItemFrameMetadata(id, entity)
	}
	if entity.Type == "minecraft:chest_minecart" {
		return proto.NewWriter().VarInt(id).U8(0xff).Payload()
	}
	if entity.Type != "minecraft:end_crystal" {
		return generatedMobMetadata(id, entity)
	}
	w := proto.NewWriter().VarInt(id)
	if beam, ok := entity.Data["beam_target"].Value.([]int32); ok && len(beam) == 3 {
		w.U8(8).VarInt(11).Bool(true).I64(proto.PackPosition(beam[0], beam[1], beam[2]))
	}
	if bottom, ok := entity.Data.Number("ShowBottom"); ok && bottom == 0 {
		w.U8(9).VarInt(8).Bool(false)
	}
	return w.U8(0xff).Payload()
}

// Fetch actor snapshots before entityMu: generation and save operations must
// never hold the server's entity publication lock. The captured epoch excludes
// an obsolete world even if the player leaves and returns to the same dimension.
func (s *Server) updateGeneratedEntityVisibility(p *player.Player) {
	revision := s.trapRevision.Load()
	if !p.PlayReady() {
		return
	}
	w := s.PlayerWorld(p)
	sink := p.WorldPacketSinkInDimension(w.DimensionName())
	if sink == nil {
		return
	}
	var chunks [][2]int32
	p.ForEachSentChunk(func(cx, cz int32) bool { chunks = append(chunks, [2]int32{cx, cz}); return true })
	desired := make(map[generatedEntityKey]world.WorldEntity)
	vehicles := make(map[generatedEntityKey][]generatedEntityKey)
	var collect func(world.WorldEntity, int)
	collect = func(entity world.WorldEntity, depth int) {
		if depth > 64 || !weatherEntityInRange(p, entity) {
			return
		}
		key := generatedEntityKey{w.DimensionName(), entity.UUID}
		if _, supported := generatedEntityTypes[entity.Type]; !supported {
			return
		}
		desired[key] = entity
		passengers, err := world.EntityPassengers(entity)
		if err != nil {
			s.Logf("generated passenger load failed: %v", err)
			return
		}
		for _, passenger := range passengers {
			if _, supported := generatedEntityTypes[passenger.Type]; supported {
				vehicles[key] = append(vehicles[key], generatedEntityKey{w.DimensionName(), passenger.UUID})
				collect(passenger, depth+1)
			}
		}
	}
	for _, chunk := range chunks {
		if !sink.Current() {
			return
		}
		entities, err := w.EntitiesInChunk(chunk[0], chunk[1])
		if err != nil {
			continue
		}
		for _, entity := range entities {
			collect(entity, 0)
		}
	}
	// A scan can overlap a whole trap activation. Publish its snapshot only
	// after that tick's ordered spawn/remove packets, and only if still current.
	s.trapPublishMu.Lock()
	defer s.trapPublishMu.Unlock()
	if revision != s.trapRevision.Load() {
		return
	}
	s.entityMu.Lock()
	defer s.entityMu.Unlock()
	if !sink.Current() || !p.PlayReady() {
		return
	}
	added := make(map[generatedEntityKey]bool)
	for key, entity := range desired {
		cx, cz := int32(math.Floor(entity.Position[0]/16)), int32(math.Floor(entity.Position[2]/16))
		if !sink.IsChunkSent(cx, cz) {
			delete(desired, key)
		}
	}
	view := s.generatedEntityView(p)
	var removed []int32
	for key, id := range view {
		if tracking := s.generatedEntities[key]; tracking != nil && tickingEntityType(tracking.snapshot.Type) {
			continue // published by the world clock from the live entity index
		}
		if _, keep := desired[key]; !keep {
			removed = append(removed, id)
			s.releaseGeneratedEntity(key)
			delete(view, key)
			p.MarkInvisible(id)
		}
	}
	if len(removed) > 0 {
		sink.SendPacket(proto.SEntityDestroy, destroyPayload(removed))
	}
	for key, entity := range desired {
		if tracking := s.generatedEntities[key]; tracking != nil {
			if tracking.snapshot.Revision > entity.Revision {
				entity = tracking.snapshot
			}
			desired[key] = entity
		}
		if _, already := view[key]; already {
			continue
		}
		s.spawnGeneratedEntity(p, sink, key, entity, view)
		added[key] = true
	}
	for vehicle, passengers := range vehicles {
		id, visible := view[vehicle]
		if !visible {
			continue
		}
		changed := added[vehicle]
		ids := make([]int32, 0, len(passengers))
		for _, passenger := range passengers {
			if child, visible := view[passenger]; visible {
				ids = append(ids, child)
				changed = changed || added[passenger]
			}
		}
		if changed && len(ids) > 0 {
			sink.SendPacket(proto.SSetPassengers, generatedPassengersPayload(id, ids))
		}
	}
}

// The caller holds entityMu and has checked chunk delivery and visibility.
func (s *Server) spawnGeneratedEntity(p *player.Player, sink *player.WorldSink, key generatedEntityKey, entity world.WorldEntity, view map[generatedEntityKey]int32) {
	tracking := s.generatedEntities[key]
	if tracking == nil {
		tracking = s.newEntityTracking(entity)
		s.generatedEntities[key] = tracking
	}
	if entity.Revision >= tracking.snapshot.Revision {
		tracking.snapshot = entity
	}
	tracking.viewers++
	view[key] = tracking.id
	sink.SendPacket(proto.SSpawnEntity, generatedEntitySpawnPayload(tracking.id, entity))
	if metadata := generatedEntityMetadata(tracking.id, entity); len(metadata) > 0 {
		sink.SendPacket(proto.SEntityMetadata, metadata)
	}
	if equipment, err := generatedEntityEquipment(tracking.id, entity); err != nil {
		s.Logf("generated entity equipment failed: entity=%s error=%v", entity.Type, err)
	} else if len(equipment) > 0 {
		sink.SendPacket(proto.SSetEquipment, equipment)
	}
	p.MarkVisible(tracking.id)
}

func (s *Server) newEntityTracking(entity world.WorldEntity) *generatedEntityTracking {
	id := entity.RuntimeID
	if id == 0 {
		id = s.nextEntityID()
	}
	return &generatedEntityTracking{id: id, snapshot: entity}
}

func (s *Server) generatedEntityView(p *player.Player) map[generatedEntityKey]int32 {
	if s.generatedViews == nil {
		s.generatedViews = make(map[*player.Player]map[generatedEntityKey]int32)
		s.generatedEntities = make(map[generatedEntityKey]*generatedEntityTracking)
	}
	view := s.generatedViews[p]
	if view == nil {
		view = make(map[generatedEntityKey]int32)
		s.generatedViews[p] = view
	}
	return view
}

func (s *Server) publishTrapStrike(players []*player.Player, dimension string, horse, bolt world.WorldEntity) {
	cx, cz := fallingChunk(horse)
	key := generatedEntityKey{dimension, horse.UUID}
	s.entityMu.Lock()
	for _, p := range players {
		sink := p.WorldPacketSinkInDimension(dimension)
		if sink == nil || !p.PlayReady() || !sink.IsChunkSent(cx, cz) || !weatherEntityInRange(p, horse) {
			continue
		}
		view := s.generatedEntityView(p)
		if _, visible := view[key]; !visible {
			s.spawnGeneratedEntity(p, sink, key, horse, view)
		}
	}
	s.entityMu.Unlock()
	register := s.publishLightningSpawn(players, dimension, bolt)
	register()
}

func generatedPassengersPayload(vehicle int32, passengers []int32) []byte {
	w := proto.NewWriter().VarInt(vehicle).VarInt(int32(len(passengers)))
	for _, id := range passengers {
		w.VarInt(id)
	}
	return w.Payload()
}

func (s *Server) releaseGeneratedEntity(key generatedEntityKey) {
	if tracking := s.generatedEntities[key]; tracking != nil {
		tracking.viewers--
		if tracking.viewers == 0 {
			if tickingEntityType(tracking.snapshot.Type) {
				s.trackedTicking--
			}
			delete(s.generatedEntities, key)
		}
	}
}

// Called under entityMu on disconnect or before Respawn. Other viewers retain
// the same entity ID; the final viewer releases the world/chunk tracking entry.
func (s *Server) clearGeneratedEntityView(p *player.Player, send bool) {
	var removed []int32
	for key, id := range s.generatedViews[p] {
		removed = append(removed, id)
		s.releaseGeneratedEntity(key)
		p.MarkInvisible(id)
	}
	delete(s.generatedViews, p)
	if send && len(removed) > 0 {
		p.SendPacket(proto.SEntityDestroy, destroyPayload(removed))
	}
}
