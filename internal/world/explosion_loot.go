package world

import (
	"atlas/internal/nbt"
	"atlas/internal/worldgen"
	"atlas/internal/worldgen/randomsource"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed blast_blocks_1.21.1.json.gz
var blastAssets []byte

type blastTable struct {
	Pools []struct {
		Conditions []blastCondition
		Entries    []blastEntry
		Functions  []blastFunction
	}
	Functions []blastFunction
}
type blastEntry struct {
	Type, Name string
	Children   []blastEntry
	Conditions []blastCondition
	Functions  []blastFunction
}
type blastCondition struct {
	Condition, Block string
	Chance           float32
	Chances          []float32
	Properties       map[string]string
	Terms            []blastCondition
	Term             *blastCondition
	OffsetY          int32
	Predicate        struct {
		Block struct {
			Blocks string
			State  map[string]string
		}
	}
}
type blastFunction struct {
	Function, Formula   string
	Include, Properties []string
	Conditions          []blastCondition
	Count               json.RawMessage
	Add                 bool
	Parameters          struct {
		Extra       int
		Probability float32
	}
	Limit struct{ Min, Max *float32 }
}
type blastData struct {
	Tables     map[string]blastTable
	Aliases    map[string]string
	Resistance [][2]float32
	byState    [MaxBlockState + 1]float32
}

var readBlastAssets = sync.OnceValues(func() (*blastData, error) {
	in, err := gzip.NewReader(bytes.NewReader(blastAssets))
	if err != nil {
		return nil, err
	}
	defer in.Close()
	var data blastData
	if err := json.NewDecoder(in).Decode(&data); err != nil {
		return nil, err
	}
	for i, pair := range data.Resistance {
		end := len(data.byState)
		if i+1 < len(data.Resistance) {
			end = int(data.Resistance[i+1][0])
		}
		for j := int(pair[0]); j < end; j++ {
			data.byState[j] = pair[1]
		}
	}
	return &data, nil
})

type blastLootContext struct {
	r     *fluidRuntime
	pos   worldgen.FeaturePosition
	state worldgen.SurfaceBlockState
	rng   *randomsource.Xoroshiro
	decay float32
	err   error
}

func blockMatches(state worldgen.SurfaceBlockState, name string, properties map[string]string) bool {
	if state.Name != name {
		return false
	}
	for key, value := range properties {
		if state.Properties[key] != value {
			return false
		}
	}
	return true
}

func (c *blastLootContext) test(condition blastCondition) bool {
	switch condition.Condition {
	case "minecraft:survives_explosion":
		return c.decay == 0 || c.rng.NextFloat() <= 1/c.decay
	case "minecraft:table_bonus":
		return c.rng.NextFloat() < condition.Chances[0]
	case "minecraft:random_chance":
		return c.rng.NextFloat() < condition.Chance
	case "minecraft:match_tool":
		return false // every shipped block predicate requires a tool or enchantment
	case "minecraft:entity_properties":
		return true // both shipped predicates ask only for the source entity
	case "minecraft:inverted":
		return !c.test(*condition.Term)
	case "minecraft:any_of":
		for _, term := range condition.Terms {
			if c.test(term) {
				return true
			}
		}
	case "minecraft:block_state_property":
		return blockMatches(c.state, condition.Block, condition.Properties)
	case "minecraft:location_check":
		p := c.pos
		p.Y += condition.OffsetY
		state, err := c.r.BlockAt(p)
		if err != nil {
			c.err = err
			return false
		}
		return blockMatches(state, condition.Predicate.Block.Blocks, condition.Predicate.Block.State)
	}
	return false
}

func (c *blastLootContext) passes(conditions []blastCondition) bool {
	for _, condition := range conditions {
		if !c.test(condition) {
			return false
		}
	}
	return true
}

func (c *blastLootContext) selectEntry(entry *blastEntry) *blastEntry {
	if !c.passes(entry.Conditions) {
		return nil
	}
	if entry.Type != "minecraft:alternatives" {
		return entry
	}
	for i := range entry.Children {
		if selected := c.selectEntry(&entry.Children[i]); selected != nil {
			return selected
		}
	}
	return nil
}

func (c *blastLootContext) count(raw json.RawMessage) int32 {
	var n float32
	if json.Unmarshal(raw, &n) == nil {
		return int32(n)
	}
	var provider struct {
		Type           string
		Min, Max, N, P float32
	}
	_ = json.Unmarshal(raw, &provider)
	if provider.Type == "minecraft:uniform" {
		lo, hi := int32(provider.Min), int32(provider.Max)
		if lo >= hi {
			return lo
		}
		return lo + c.rng.NextIntBound(hi-lo+1)
	}
	return c.binomial(int(provider.N), provider.P)
}

func (c *blastLootContext) binomial(n int, chance float32) int32 {
	var count int32
	for i := 0; i < n; i++ {
		if c.rng.NextFloat() < chance {
			count++
		}
	}
	return count
}

func (c *blastLootContext) apply(count int32, functions []blastFunction) int32 {
	for _, f := range functions {
		if !c.passes(f.Conditions) {
			continue
		}
		switch f.Function {
		case "minecraft:set_count":
			n := c.count(f.Count)
			if f.Add {
				count += n
			} else {
				count = n
			}
		case "minecraft:explosion_decay":
			if c.decay != 0 {
				var kept int32
				for i := int32(0); i < count; i++ {
					if c.rng.NextFloat() <= 1/c.decay {
						kept++
					}
				}
				count = kept
			}
		case "minecraft:apply_bonus":
			switch f.Formula {
			case "minecraft:uniform_bonus_count":
				c.rng.NextIntBound(1)
			case "minecraft:binomial_with_bonus_count":
				count += c.binomial(f.Parameters.Extra, f.Parameters.Probability)
			}
		case "minecraft:limit_count":
			if f.Limit.Min != nil {
				count = max(count, int32(*f.Limit.Min))
			}
			if f.Limit.Max != nil {
				count = min(count, int32(*f.Limit.Max))
			}
		}
	}
	return count
}

func (r *fluidRuntime) blastLoot(p worldgen.FeaturePosition, state worldgen.SurfaceBlockState, decay float32, be nbt.Object) ([]blastDrop, error) {
	assets, err := readBlastAssets()
	if err != nil {
		return nil, err
	}
	name := strings.TrimPrefix(state.Name, "minecraft:")
	if key, ok := assets.Aliases[name]; ok {
		name = strings.TrimPrefix(key, "blocks/")
	}
	table, ok := assets.Tables[name]
	if !ok || len(table.Pools) == 0 || state.Name == "minecraft:tnt" {
		return nil, nil
	}
	c := blastLootContext{r: r, pos: p, state: state, decay: decay}
	var result []blastDrop
	err = r.world.randomSequences.withSequence("minecraft:blocks/"+name, func(rng *randomsource.Xoroshiro) error {
		c.rng = rng
		for _, pool := range table.Pools {
			// Every 1.21.1 block pool has one roll and one root entry.
			if !c.passes(pool.Conditions) {
				continue
			}
			entry := c.selectEntry(&pool.Entries[0])
			if entry == nil {
				continue
			}
			if entry.Type == "minecraft:dynamic" {
				sherds, _ := be["sherds"].Elements()
				for i := 0; i < 4; i++ {
					name := "minecraft:brick"
					if i < len(sherds) {
						if s, ok := sherds[i].Value.(string); ok {
							name = s
						}
					}
					result = append(result, blastDrop{leafDrop: leafDrop{name, 1}, pos: p})
				}
				continue
			}
			n := c.apply(1, entry.Functions)
			n = c.apply(n, pool.Functions)
			n = c.apply(n, table.Functions)
			result = append(result, blastDrop{leafDrop: leafDrop{entry.Name, n}, pos: p, components: blastComponents(entry.Functions, be, state)})
		}
		return c.err
	})
	return result, err
}
