package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
)

const leafDecayJournal = "AtlasPendingLeafDecay"

func (r *fluidRuntime) decayLeaf(p worldgen.FeaturePosition, state worldgen.SurfaceBlockState) error {
	key := fmt.Sprintf("%d,%d,%d", p.X, p.Y, p.Z)
	chunk := chunkKey{p.X >> 4, p.Z >> 4}
	var pending nbt.Object
	if err := r.world.fallingEntityOperation(chunk.x, chunk.z, func(_ *Region, ch *Chunk) error {
		root, _ := ch.anvilRoot.Object()
		entries, _ := root.Object(leafDecayJournal)
		pending, _ = entries.Object(key)
		return nil
	}); err != nil {
		return err
	}
	if pending != nil {
		return r.finishLeafDecay(chunk, key, pending)
	}
	drops, err := r.world.randomSequences.leafDrops(state.Name)
	if err != nil {
		return err
	}
	var items []nbt.Tag
	if len(drops) > 0 {
		enabled := strings.EqualFold(r.world.gameRule("doTileDrops", "true"), "true")
		for _, drop := range drops {
			var entityRandom *worldgen.LegacyRandom
			if enabled {
				var seed [8]byte
				if _, err := rand.Read(seed[:]); err != nil {
					return err
				}
				entityRandom = worldgen.NewLegacyRandom(int64(binary.LittleEndian.Uint64(seed[:])))
			}
			item, err := worldgen.NewBlockDropEntity(p, drop.item, drop.count, enabled, r.world.tickRandom, entityRandom)
			if err != nil {
				return err
			}
			if item != nil {
				items = append(items, nbt.Tag{Type: nbt.Compound, Value: item})
			}
		}
	}
	if len(items) == 0 {
		return r.removeDecayedLeaf(p, state)
	}
	id, err := BlockStateID(state.Name, state.Properties)
	if err != nil {
		return err
	}
	entry := nbt.Object{
		"Source":   {Type: nbt.IntArray, Value: []int32{p.X, p.Y, p.Z}},
		"Expected": {Type: nbt.Int, Value: int32(id)},
		"Items":    {Type: nbt.List, ListType: nbt.Compound, Value: items},
	}
	if err := r.world.fallingEntityOperation(chunk.x, chunk.z, func(actor *Region, ch *Chunk) error {
		return updateDropJournal(actor, ch, leafDecayJournal, key, entry)
	}); err != nil {
		return err
	}
	return r.finishLeafDecay(chunk, key, entry)
}

func (r *fluidRuntime) removeDecayedLeaf(p worldgen.FeaturePosition, state worldgen.SurfaceBlockState) error {
	fluid, err := worldgen.FluidStateForBlock(state)
	if err != nil {
		return err
	}
	return r.SetFluidBlock(p, fluid.LegacyBlock(), 3)
}

func (r *fluidRuntime) finishLeafDecay(chunk chunkKey, key string, entry nbt.Object) error {
	xyz, _ := entry["Source"].Value.([]int32)
	id, ok := entry.Number("Expected")
	if len(xyz) != 3 || !ok || id < 0 || id > MaxBlockState || randomHandlers[id] != tickLeaf {
		return fmt.Errorf("invalid leaf decay record %s", key)
	}
	p := worldgen.FeaturePosition{X: xyz[0], Y: xyz[1], Z: xyz[2]}
	if p.X>>4 != chunk.x || p.Z>>4 != chunk.z || !r.InWorld(p) || key != fmt.Sprintf("%d,%d,%d", p.X, p.Y, p.Z) {
		return fmt.Errorf("invalid leaf decay location %s", key)
	}
	items, ok := entry["Items"].Elements()
	if !ok || len(items) == 0 {
		return fmt.Errorf("invalid leaf decay items")
	}
	if err := r.world.fallingEntityOperation(chunk.x, chunk.z, func(actor *Region, ch *Chunk) error {
		entities := append([]WorldEntity(nil), ch.entities...)
		for _, item := range items {
			data, _ := item.Object()
			e, err := worldEntityFromNBT(data)
			if err != nil {
				return err
			}
			if e.Type != "minecraft:item" {
				return fmt.Errorf("invalid leaf drop entity")
			}
			found := false
			for _, prev := range entities {
				if prev.UUID == e.UUID {
					found = true
					break
				}
			}
			if !found {
				entities = append(entities, e)
			}
		}
		if len(entities) == len(ch.entities) {
			return nil
		}
		return persistChunkEntities(actor, ch, entities)
	}); err != nil {
		return err
	}
	current, err := r.world.ReadLoadedBlocks([]BlockPosition{BlockPosition(p)})
	if err != nil {
		return err
	}
	if current[0] == uint32(id) {
		state, err := BlockStateByID(uint32(id))
		if err != nil {
			return err
		}
		if err := r.removeDecayedLeaf(p, worldgen.SurfaceBlockState{Name: state.Name, Properties: state.Properties}); err != nil {
			return err
		}
	}
	return r.world.fallingEntityOperation(chunk.x, chunk.z, func(actor *Region, ch *Chunk) error { return updateDropJournal(actor, ch, leafDecayJournal, key, nil) })
}
