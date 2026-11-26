package world

import (
	"atlas/internal/item"
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func (w *World) ExplosionRule(name string) bool {
	return strings.EqualFold(w.gameRule(name, strconv.FormatBool(name != "tntExplosionDropDecay")), "true")
}

func (w *World) SetExplosionRule(name string, enabled bool) error {
	switch name {
	case "mobGriefing", "mobExplosionDropDecay", "tntExplosionDropDecay", "doTileDrops":
	default:
		return fmt.Errorf("unknown explosion rule %s", name)
	}
	value := strconv.FormatBool(enabled)
	return w.setCycleRule(name, value, func(owner *World) {
		if owner.store == nil {
			if owner.ruleOverrides == nil {
				owner.ruleOverrides = make(map[string]string)
			}
			owner.ruleOverrides[name] = value
		}
	})
}

func (r *fluidRuntime) traceBlast(center [3]float64, power float32) ([]worldgen.FeaturePosition, error) {
	assets, err := readBlastAssets()
	if err != nil {
		return nil, err
	}
	// Snapshot each crossed section once, before any block callbacks run.
	cache := make(map[worldgen.FeaturePosition]*Section)
	return worldgen.ExplosionBlocks(center, power, r.world.tickRandom, func(p worldgen.FeaturePosition) (float32, bool, error) {
		if !r.InWorld(p) {
			return 0, false, nil
		}
		key := worldgen.FeaturePosition{X: p.X >> 4, Y: p.Y >> 4, Z: p.Z >> 4}
		section, ok := cache[key]
		if !ok {
			read := func(_ *Region, ch *Chunk) error {
				if src := ch.sect[ch.sectionOf(p.Y)]; src != nil {
					section = &Section{bits: src.bits, palette: slices.Clone(src.palette), data: slices.Clone(src.data)}
				}
				return nil
			}
			err := r.world.fallingEntityOperation(key.X, key.Z, read)
			if errors.Is(err, ErrChunkNotLoaded) {
				if _, err = r.world.LoadChunk(key.X, key.Z); err == nil {
					err = r.world.fallingEntityOperation(key.X, key.Z, read)
				}
			}
			if err != nil {
				return 0, false, err
			}
			cache[key] = section
		}
		var id uint32
		if section != nil {
			id = section.get(localIndexOf(int(p.X&15), int(p.Y), int(p.Z&15)))
		}
		return assets.byState[id], true, nil
	})
}

type blastDrop struct {
	leafDrop
	pos        worldgen.FeaturePosition
	components nbt.Object
}

func mergeBlastDrop(stacks []blastDrop, drop blastDrop) []blastDrop {
	// ExplosionDecay can yield an empty stack. Vanilla retains the first
	// empty pair and Block.popResource still samples its spawn coordinates.
	if drop.count <= 0 {
		for _, s := range stacks {
			if s.count == 0 {
				return stacks
			}
		}
		drop.item = "minecraft:air"
		drop.count = 0
		return append(stacks, drop)
	}
	id, ok := item.ID(drop.item)
	if !ok {
		return stacks
	}
	maxStack := item.New(id, 1).MaxCount()
	limit := min(int32(16), maxStack)
	for i := range stacks {
		stack := &stacks[i]
		if stack.item != drop.item || stack.count >= limit || stack.count+drop.count > maxStack || !reflect.DeepEqual(stack.components, drop.components) {
			continue
		}
		n := min(drop.count, limit-stack.count)
		stack.count += n
		drop.count -= n
		if drop.count == 0 {
			return stacks
		}
	}
	return append(stacks, drop)
}

func (r *fluidRuntime) finishBlast(source WorldEntity, event *ExplosionEvent, cells []worldgen.FeaturePosition, publish func(LivingDamageChange)) (failure error) {
	w := r.world
	decayRule := "mobExplosionDropDecay"
	if source.Type == TNTEntity {
		decayRule = "tntExplosionDropDecay"
	} else if !w.ExplosionRule("mobGriefing") {
		return nil
	}
	event.BlockInteraction = 1
	var decay float32
	if w.ExplosionRule(decayRule) {
		event.BlockInteraction = 2
		decay = event.Power
	}
	for i := len(cells); i > 1; i-- {
		j := w.tickRandom.NextIntBound(int32(i))
		cells[i-1], cells[j] = cells[j], cells[i-1]
	}
	event.Affected = cells
	// Neighbour callbacks still see every edit in shuffled order. Write each
	// touched chunk once, including edits made before a later callback fails.
	r.blastChunks = make(map[chunkKey]bool)
	defer func() {
		for key := range r.blastChunks {
			err := w.fallingEntityOperation(key.x, key.z, func(_ *Region, ch *Chunk) error {
				if w.store == nil {
					return nil
				}
				return w.store.vanilla.persistChunk(ch)
			})
			failure = errors.Join(failure, err)
		}
		if failure != nil {
			// Individual committed edits were already sent by the block runtime.
			event.Affected = nil
			event.BlockInteraction = 0
		}
	}()
	var stacks []blastDrop
	var entities []WorldEntity
	defer func() { failure = errors.Join(failure, w.spawnBlastDrops(stacks, entities, publish)) }()
	air := worldgen.SurfaceBlockState{Name: "minecraft:air"}
	for _, p := range cells {
		state, err := r.BlockAt(p)
		if err != nil {
			return err
		}
		if state.Name == "minecraft:air" || state.Name == "minecraft:cave_air" || state.Name == "minecraft:void_air" {
			continue
		}
		if state.Name == TNTEntity {
			if err := r.SetFluidBlock(p, air, 3); err != nil {
				return err
			}
			tnt, err := w.primeBlastTNT(p, source)
			if err != nil {
				return err
			}
			if err := w.addExplosionEntities(chunkKey{p.X >> 4, p.Z >> 4}, []WorldEntity{tnt}, publish); err != nil {
				return err
			}
			continue
		}
		blockEntity, err := r.blastBlockEntity(p)
		if err != nil {
			return err
		}
		drops, err := r.blastLoot(p, state, decay, blockEntity)
		if err != nil {
			return err
		}
		contents, err := r.spillBlastContainer(p, state.Name, blockEntity)
		if err != nil {
			return err
		}
		if err := r.SetFluidBlock(p, air, 3); err != nil {
			return err
		}
		for _, drop := range drops {
			stacks = mergeBlastDrop(stacks, drop)
		}
		entities = append(entities, contents...)
	}
	return nil
}

func (w *World) spawnBlastDrops(stacks []blastDrop, entities []WorldEntity, publish func(LivingDamageChange)) error {
	enabled := w.ExplosionRule("doTileDrops")
	for _, stack := range stacks {
		data, err := worldgen.NewBlockDropEntity(stack.pos, stack.item, stack.count, enabled && stack.count > 0, w.tickRandom, worldgen.NewLegacyRandom(rand.Int64()))
		if err != nil {
			return err
		}
		if data == nil {
			continue
		}
		if len(stack.components) > 0 {
			saved, _ := data.Object("Item")
			saved["components"] = nbt.Tag{Type: nbt.Compound, Value: stack.components}
		}
		e, err := worldEntityFromNBT(data)
		if err != nil {
			return err
		}
		entities = append(entities, e)
	}
	groups := make(map[chunkKey][]WorldEntity)
	for _, e := range entities {
		key := explosionActorChunk(e.Position)
		e.RuntimeID = NextRuntimeEntityID()
		groups[key] = append(groups[key], e)
	}
	var failure error
	for key, added := range groups {
		failure = errors.Join(failure, w.addExplosionEntities(key, added, publish))
	}
	return failure
}

func (w *World) addExplosionEntities(key chunkKey, added []WorldEntity, publish func(LivingDamageChange)) error {
	if err := w.fallingEntityOperation(key.x, key.z, func(actor *Region, ch *Chunk) error {
		updated := append(slices.Clone(ch.entities), added...)
		if w.store != nil {
			return persistChunkEntities(actor, ch, updated)
		}
		ch.entities = updated
		ch.entitiesDirty = true
		ch.indexLivingDamage()
		return nil
	}); err != nil {
		return err
	}
	if publish != nil {
		for _, e := range added {
			publish(LivingDamageChange{Entity: e, Spawned: true})
		}
	}
	return nil
}
