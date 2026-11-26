package world

import (
	"atlas/internal/javasave"
	"atlas/internal/nbt"
	"atlas/internal/worldgen/randomsource"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// All dimensions of one save use the overworld's named streams.
type lootSequences struct {
	mu               sync.Mutex
	dir              string
	seed             int64
	root             nbt.Tag
	name             string
	streams          nbt.Object
	seedBase         int64
	includeID, dirty bool
}

func (s *lootSequences) load() error {
	if s.streams != nil {
		return nil
	}
	var root nbt.Tag
	var data, streams nbt.Object
	if s.dir != "" {
		b, err := javasave.ReadGzip(filepath.Join(s.dir, "data", "random_sequences.dat"))
		if err == nil {
			s.name, root, err = nbt.Parse(b)
			if err != nil {
				return err
			}
			obj, _ := root.Object()
			var ok bool
			if data, ok = obj.Object("data"); !ok {
				return fmt.Errorf("invalid random_sequences data")
			}
			if streams, ok = data.Object("sequences"); !ok {
				return fmt.Errorf("invalid random_sequences sequences")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if data == nil {
		streams = nbt.Object{}
		data = nbt.Object{
			"sequences":           {Type: nbt.Compound, Value: streams},
			"salt":                {Type: nbt.Int, Value: int32(0)},
			"include_world_seed":  {Type: nbt.Byte, Value: int8(1)},
			"include_sequence_id": {Type: nbt.Byte, Value: int8(1)},
		}
		root = nbt.Tag{Type: nbt.Compound, Value: nbt.Object{
			"DataVersion": {Type: nbt.Int, Value: int32(3955)},
			"data":        {Type: nbt.Compound, Value: data},
		}}
	}
	salt, _ := data.Number("salt")
	s.seedBase = int64(int32(salt))
	if on, ok := data.Number("include_world_seed"); !ok || on != 0 {
		s.seedBase ^= s.seed
	}
	on, ok := data.Number("include_sequence_id")
	s.includeID = !ok || on != 0
	s.root, s.streams = root, streams
	return nil
}

type leafDrop struct {
	item  string
	count int32
}

func (s *lootSequences) leafDrops(name string) ([]leafDrop, error) {
	var drops []leafDrop
	err := s.withSequence("minecraft:blocks/"+strings.TrimPrefix(name, "minecraft:"), func(rng *randomsource.Xoroshiro) error {
		drops = rollLeafDrops(name, rng)
		return nil
	})
	return drops, err
}

func (s *lootSequences) withSequence(key string, roll func(*randomsource.Xoroshiro) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	var rng *randomsource.Xoroshiro
	entry := nbt.Object{}
	if saved, ok := s.streams[key]; ok {
		entry, _ = saved.Object()
		words, ok := entry["source"].Longs()
		if !ok || len(words) != 2 {
			return fmt.Errorf("invalid random sequence %s", key)
		}
		rng = randomsource.NewXoroshiroPair(uint64(words[0]), uint64(words[1]))
	} else {
		rng = randomsource.NewNamedSequence(s.seedBase, key, s.includeID)
	}
	if err := roll(rng); err != nil {
		return err
	}
	lo, hi := rng.State()
	entry["source"] = nbt.Tag{Type: nbt.LongArray, Value: []int64{int64(lo), int64(hi)}}
	s.streams[key] = nbt.Tag{Type: nbt.Compound, Value: entry}
	s.dirty = true
	return nil
}

func rollLeafDrops(name string, rng *randomsource.Xoroshiro) []leafDrop {
	base := strings.TrimSuffix(name, "_leaves")
	var drops []leafDrop
	if base != "minecraft:mangrove" {
		chance := float32(0.05)
		if base == "minecraft:jungle" {
			chance = 0.025
		}
		if rng.NextFloat() < chance {
			item := base
			if !strings.HasSuffix(base, "azalea") {
				item += "_sapling"
			}
			drops = append(drops, leafDrop{item, 1})
		}
	}
	if rng.NextFloat() < 0.02 {
		drops = append(drops, leafDrop{"minecraft:stick", 1 + rng.NextIntBound(2)})
	}
	if (base == "minecraft:oak" || base == "minecraft:dark_oak") && rng.NextFloat() < 0.005 {
		drops = append(drops, leafDrop{"minecraft:apple", 1})
	}
	return drops
}

func (s *lootSequences) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty || s.dir == "" {
		return nil
	}
	dir := filepath.Join(s.dir, "data")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := javasave.WriteNBT(filepath.Join(dir, "random_sequences.dat"), s.name, s.root); err != nil {
		return err
	}
	s.dirty = false
	return nil
}
