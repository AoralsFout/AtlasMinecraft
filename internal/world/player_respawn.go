package world

import (
	"atlas/internal/blockrules"
	"atlas/internal/worldgen"
	"context"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
)

func (w *World) centeredSpawn(p BlockPosition, dy float64, yaw float32) PlayerPosition {
	return PlayerPosition{Dimension: w.DimensionName(), X: float64(p.X) + .5, Y: float64(p.Y) + dy, Z: float64(p.Z) + .5, Yaw: yaw}
}

// SharedRespawn follows ServerPlayer.adjustSpawnLocation's randomized column
// walk, then settles vertically at the shared spawn if no column qualified.
func (w *World) SharedRespawn(ctx context.Context) (PlayerPosition, error) {
	x, y, z, _ := w.SpawnPoint()
	border, err := w.PortalBorder()
	if err != nil {
		return PlayerPosition{}, err
	}
	rule, err := strconv.ParseInt(w.gameRule("spawnRadius", "10"), 10, 32)
	if err != nil {
		return PlayerPosition{}, err
	}
	radius := max(int64(0), rule)
	distance := int64(math.Floor(min(float64(x)-border.MinX, border.MaxX-float64(x), float64(z)-border.MinZ, border.MaxZ-float64(z))))
	radius = min(radius, distance)
	if distance <= 1 {
		radius = 1
	}
	adventure := false
	if w.store != nil {
		v := w.store.vanilla
		v.mu.Lock()
		root, _ := v.root.Object()
		data, _ := root.Object("Data")
		mode, _ := data.Number("GameType")
		adventure = mode == 2
		v.mu.Unlock()
	}
	if !adventure {
		width := radius*2 + 1
		count := min(width*width, int64(math.MaxInt32))
		step := int64(17)
		if count <= 16 {
			step = count - 1
		}
		start := rand.Int64N(count)
		for i := int64(0); i < count; i++ {
			index := (start + step*i) % count
			p := BlockPosition{X: x + int32(index%width-radius), Z: z + int32(index/width-radius)}
			if err := w.ensureChunk(ctx, p.X>>4, p.Z>>4); err != nil {
				return PlayerPosition{}, err
			}
			col, err := w.scanSpawn(ctx, p.X>>4, p.Z>>4, &p)
			if err != nil {
				return PlayerPosition{}, err
			}
			if col.ok {
				p.Y = col.y
				pos := w.centeredSpawn(p, 0, 0)
				boxes, err := w.collisionBoxes(playerBox(pos.X, pos.Y, pos.Z, 1.8))
				if err != nil {
					return PlayerPosition{}, err
				}
				if len(boxes) == 0 {
					return pos, nil
				}
			}
		}
	}
	if err := w.ensureChunk(ctx, x>>4, z>>4); err != nil {
		return PlayerPosition{}, err
	}
	clearAt := func(y int32) (bool, error) {
		boxes, err := w.collisionBoxes(playerBox(float64(x)+.5, float64(y), float64(z)+.5, 1.8))
		return len(boxes) == 0, err
	}
	for y < w.MaxBuildHeight()-1 {
		clear, err := clearAt(y)
		if err != nil {
			return PlayerPosition{}, err
		}
		if clear {
			break
		}
		y++
	}
	for y > w.MinBuildHeight()+1 {
		clear, err := clearAt(y - 1)
		if err != nil {
			return PlayerPosition{}, err
		}
		if !clear {
			break
		}
		y--
	}
	return w.centeredSpawn(BlockPosition{x, y, z}, 0, 0), nil
}

// ResolvePersonalSpawn returns a charge edit only for a later End return
// through a non-forced anchor. The caller commits it after preparing terrain.
func (w *World) ResolvePersonalSpawn(p PersonalSpawn, afterCredits bool) (pos PlayerPosition, spendCharge func() error, found bool, err error) {
	if p.Y < w.MinBuildHeight() || p.Y >= w.MaxBuildHeight() || p.X < -30000000 || p.X >= 30000000 || p.Z < -30000000 || p.Z >= 30000000 {
		return
	}
	id, err := w.BlockAt(p.X, p.Y, p.Z)
	if err != nil {
		return
	}
	block, err := BlockStateByID(id)
	if err != nil {
		return
	}
	var offsets []BlockPosition
	charges, _ := strconv.Atoi(block.Properties["charges"])
	switch {
	case block.Name == "minecraft:respawn_anchor" && w.DimensionName() == "minecraft:the_nether" && (p.Forced || charges > 0):
		ring := [][2]int32{{0, -1}, {-1, 0}, {0, 1}, {1, 0}, {-1, -1}, {1, -1}, {-1, 1}, {1, 1}}
		for _, dy := range []int32{0, -1, 1} {
			for _, d := range ring {
				offsets = append(offsets, BlockPosition{d[0], dy, d[1]})
			}
		}
		offsets = append(offsets, BlockPosition{Y: 1})
		if !p.Forced && !afterCredits {
			next, e := BlockStateID(block.Name, map[string]string{"charges": strconv.Itoa(charges - 1)})
			if e != nil {
				return pos, nil, false, e
			}
			spendCharge = func() error { return w.TryPlaceBlock(p.BlockPosition, id, next, nil) }
		}
	case strings.HasSuffix(block.Name, "_bed") && w.DimensionName() == "minecraft:overworld":
		below, e := w.BlockAt(p.X, p.Y-1, p.Z)
		if e != nil {
			return pos, nil, false, e
		}
		support, e := BlockStateByID(below)
		if e != nil {
			return pos, nil, false, e
		}
		offsets = bedRespawnOffsets(block.Properties["facing"], p.Angle, strings.HasSuffix(support.Name, "_bed"))
	default:
		if !p.Forced {
			return
		}
		above, e := w.BlockAt(p.X, p.Y+1, p.Z)
		if e != nil {
			return pos, nil, false, e
		}
		for _, state := range []uint32{id, above} {
			r, e := blockrules.Lookup(state)
			if e != nil || !r.CanRespawnInside {
				return pos, nil, false, e
			}
		}
		return w.centeredSpawn(p.BlockPosition, .1, p.Angle), nil, true, nil
	}
	border, err := w.PortalBorder()
	if err != nil {
		return pos, nil, false, err
	}
	for _, strict := range []bool{true, false} {
		for _, d := range offsets {
			point := BlockPosition{p.X + d.X, p.Y + d.Y, p.Z + d.Z}
			dy, ok, e := w.respawnFloor(point, border, strict)
			if e != nil {
				return pos, nil, false, e
			}
			if ok {
				return w.centeredSpawn(point, dy, respawnYaw(d, dy)), spendCharge, true, nil
			}
		}
	}
	return pos, nil, false, nil
}

// ServerPlayer.RespawnPosAngle uses Mth.atan2 on the normalized direction
// back to the spawn block. Its inverse-square-root estimate and asin lookup
// produce observable float differences in the position packet.
func respawnYaw(offset BlockPosition, floor float64) float32 {
	x, y, z := -float64(offset.X), -float64(offset.Y)-floor, -float64(offset.Z)
	length := math.Sqrt(x*x + y*y + z*z)
	if length < 1e-4 || x == 0 && z == 0 {
		return -90
	}
	x, z = x/length, z/length
	square := x*x + z*z
	inv := math.Float64frombits(6910469410427058090 - (math.Float64bits(square) >> 1))
	inv *= 1.5 - .5*square*inv*inv
	major, minor := max(math.Abs(x), math.Abs(z))*inv, min(math.Abs(x), math.Abs(z))*inv
	sample := math.RoundToEven(minor*256) / 256
	base := math.Asin(sample)
	diff := minor*math.Cos(base) - major*sample
	angle := base + (6+diff*diff)*diff/6
	if math.Abs(z) > math.Abs(x) {
		angle = math.Pi/2 - angle
	}
	if x < 0 {
		angle = math.Pi - angle
	}
	if z < 0 {
		angle = -angle
	}
	yaw := angle*57.2957763671875 - 90
	if yaw < -180 {
		yaw += 360
	}
	return float32(yaw)
}

func bedRespawnOffsets(facing string, angle float32, bunk bool) []BlockPosition {
	dirs := map[string][2]int32{"north": {0, -1}, "south": {0, 1}, "east": {1, 0}, "west": {-1, 0}}
	f := dirs[facing]
	sx, sz := -f[1], f[0]
	// Direction.isFacingAngle uses the 65536-entry Mth sine table.
	phase := angle * float32(math.Pi/180) * float32(10430.378)
	sine := func(index float32) float64 { return math.Sin(float64(uint16(int32(index))) * 2 * math.Pi / 65536) }
	if -sine(phase)*float64(sx)+sine(phase+16384)*float64(sz) > 0 {
		sx, sz = -sx, -sz
	}
	var offsets []BlockPosition
	layers := []int32{0}
	if bunk {
		layers = append(layers, -1)
	}
	for _, dy := range layers {
		for _, pair := range [][2]int32{{1, 0}, {1, -1}, {1, -2}, {0, -2}, {-1, -2}, {-1, -1}, {-1, 0}, {-1, 1}, {0, 1}, {1, 1}} {
			offsets = append(offsets, BlockPosition{sx*pair[0] + f[0]*pair[1], dy, sz*pair[0] + f[1]*pair[1]})
		}
	}
	return append(offsets, BlockPosition{}, BlockPosition{X: -f[0], Z: -f[1]})
}

func (w *World) respawnFloor(p BlockPosition, border worldgen.PortalBorder, strict bool) (float64, bool, error) {
	x, z := float64(p.X)+.5, float64(p.Z)+.5
	if x-.3 < border.MinX || x+.3 > border.MaxX || z-.3 < border.MinZ || z+.3 > border.MaxZ {
		return 0, false, nil
	}
	if err := w.ensureChunk(context.Background(), p.X>>4, p.Z>>4); err != nil {
		return 0, false, err
	}
	ids, err := w.ReadLoadedBlocks([]BlockPosition{p, {p.X, p.Y - 1, p.Z}, {p.X, p.Y + 1, p.Z}})
	if err != nil {
		return 0, false, err
	}
	var states [3]BlockState
	for i, id := range ids {
		states[i], err = BlockStateByID(id)
		if err != nil {
			return 0, false, err
		}
	}
	for _, state := range []BlockState{states[0], states[2]} {
		if state.Name == "minecraft:end_portal" || state.Name == "minecraft:end_gateway" {
			return 0, false, nil
		}
	}
	dy, err := dismountShapeHeight(ids[0], states[0])
	if err != nil {
		return 0, false, err
	}
	if math.IsInf(dy, -1) {
		below, err := dismountShapeHeight(ids[1], states[1])
		if err != nil {
			return 0, false, err
		}
		if below < 1 {
			return 0, false, nil
		}
		dy = below - 1
	}
	if dy >= 1 {
		return 0, false, nil
	}
	if strict {
		danger := func(i int) bool { return spawnHazard(ids[i]) || states[i].Name == "minecraft:lava" }
		if danger(0) || dy <= 0 && danger(1) {
			return 0, false, nil
		}
	}
	boxes, err := w.collisionBoxes(playerBox(x, float64(p.Y)+dy, z, 1.8))
	return dy, len(boxes) == 0, err
}

func dismountShapeHeight(id uint32, state BlockState) (float64, error) {
	switch state.Name {
	case "minecraft:ladder", "minecraft:vine", "minecraft:scaffolding", "minecraft:weeping_vines", "minecraft:weeping_vines_plant", "minecraft:twisting_vines", "minecraft:twisting_vines_plant", "minecraft:cave_vines", "minecraft:cave_vines_plant":
		return math.Inf(-1), nil
	}
	if strings.HasSuffix(state.Name, "_trapdoor") && state.Properties["open"] == "true" {
		return math.Inf(-1), nil
	}
	rules, err := blockrules.Lookup(id)
	if err != nil {
		return 0, err
	}
	top := math.Inf(-1)
	for _, box := range rules.Boxes {
		top = max(top, box[4])
	}
	return top, nil
}
