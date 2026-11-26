package world

import (
	"atlas/internal/blockrules"
	"errors"
	"math"
)

// Explosion.getSeenPercent samples the target's collision box, including the
// half-remainder offsets on X and Z. Fluids do not occlude these rays.
func (w *World) blastExposure(center, pos [3]float64, width, height float64) (float64, error) {
	dx, dy := 1/(2*width+1), 1/(2*height+1)
	offset := (1 - math.Floor(1/dx)*dx) / 2
	var bounds blockrules.Box
	low := [3]float64{pos[0] - width/2 + offset, pos[1], pos[2] - width/2 + offset}
	size := [3]float64{width, height, width}
	local := true
	for i := range center {
		bounds[i] = min(low[i], center[i]) - 1e-6
		bounds[i+3] = max(low[i]+size[i], center[i]) + 1e-6
		local = local && bounds[i+3]-bounds[i] < 24
	}
	var boxes []blockrules.Box
	if local {
		var err error
		boxes, err = w.collisionBoxes(bounds)
		if errors.Is(err, ErrChunkNotLoaded) {
			local = false
		} else if err != nil {
			return 0, err
		} else if len(boxes) == 0 {
			return 1, nil
		}
	}
	seen, total := 0, 0
	for x := 0.; x <= 1; x += dx {
		for y := 0.; y <= 1; y += dy {
			for z := 0.; z <= 1; z += dx {
				point := [3]float64{low[0] + x*width, low[1] + y*height, low[2] + z*width}
				clear := clearThroughBoxes(point, center, boxes)
				if !local {
					var err error
					clear, err = w.clearCollisionRay(point, center)
					if err != nil {
						return 0, err
					}
				}
				if clear {
					seen++
				}
				total++
			}
		}
	}
	return float64(float32(seen) / float32(total)), nil
}

func (w *World) creeperBlast(change LivingDamageChange, publish func(LivingDamageChange)) error {
	center := change.Entity.Position
	radius := float64(change.Fuse.Power) * 2
	if radius <= 0 {
		return nil
	}
	impact := func(pos [3]float64, width, height float64) (float64, bool, error) {
		dx, dy, dz := pos[0]-center[0], pos[1]-center[1], pos[2]-center[2]
		distance := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if distance > radius {
			return 0, false, nil
		}
		exposure, err := w.blastExposure(center, pos, width, height)
		return (1 - distance/radius) * exposure, true, err
	}
	o := w.scheduledTicks
	o.mu.Lock()
	var chunks []*chunkScheduledTicks
	for _, h := range o.loadOrder {
		x, z := float64(h.x)*16, float64(h.z)*16
		if h.hasLiving && x <= center[0]+radius && x+16 >= center[0]-radius && z <= center[2]+radius && z+16 >= center[2]-radius {
			chunks = append(chunks, h)
		}
	}
	o.mu.Unlock()
	for _, h := range chunks {
		mobs, err := w.EntitiesInChunk(h.x, h.z)
		if err != nil {
			return err
		}
		damage := make(map[[16]byte]float32)
		for len(mobs) > 0 {
			e := mobs[len(mobs)-1]
			mobs = mobs[:len(mobs)-1]
			children, err := EntityPassengers(e)
			if err != nil {
				return err
			}
			mobs = append(mobs, children...)
			if _, ok := livingDefaults.Mobs[e.Type]; !ok {
				continue
			}
			if hp, _ := e.Data["Health"].Value.(float32); hp <= 0 {
				continue
			}
			width, height := livingSize(e)
			exposure, hit, err := impact(e.Position, width, height)
			if err != nil {
				return err
			}
			if hit {
				damage[e.UUID] = float32((exposure*exposure+exposure)/2*7*radius + 1)
			}
		}
		if len(damage) > 0 {
			if err := w.updateLivingChunk(h, livingUpdate{blast: damage, sourceEntity: change.Entity.RuntimeID}, nil, publish); err != nil {
				return err
			}
		}
	}
	for _, p := range w.simulationPlayers {
		if p.Spectator || p.Creative && p.Flying {
			continue
		}
		height, eye := float32(1.8), float32(1.62)
		if p.Crouching {
			height, eye = 1.5, 1.27
		}
		strength, hit, err := impact([3]float64{p.X, p.Y, p.Z}, float64(float32(.6)), float64(height))
		if err != nil {
			return err
		}
		if !hit || strength == 0 {
			continue
		}
		x, y, z := p.X-center[0], p.Y+float64(eye)-center[1], p.Z-center[2]
		length := math.Sqrt(x*x + y*y + z*z)
		if length == 0 {
			continue
		}
		if change.Fuse.PlayerImpulse == nil {
			change.Fuse.PlayerImpulse = make(map[int32][3]float32)
		}
		change.Fuse.PlayerImpulse[p.ID] = [3]float32{float32(x / length * strength), float32(y / length * strength), float32(z / length * strength)}
	}
	return nil
}
