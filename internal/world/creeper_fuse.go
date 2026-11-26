package world

import (
	"atlas/internal/blockrules"
	"atlas/internal/worldgen"
	"errors"
	"math"
	"math/rand/v2"
	"slices"
)

// CreeperFuseEvent contains the visible changes from one Creeper.tick.
// Power is the argument to Level.explode and can be zero for a custom creeper.
type CreeperFuseEvent struct {
	DirectionChanged, Primed, Exploded bool
	ExplosionEvent
}

type creeperFuse struct {
	ticks, swell, direction int32
	target, goalTarget      int32
	running                 bool
	unseen                  int
	random                  worldgen.LegacyRandom
}

func newCreeperFuse(seed int64) creeperFuse {
	return creeperFuse{direction: -1, target: -1, goalTarget: -1, random: *worldgen.NewLegacyRandom(seed)}
}

func (e WorldEntity) CreeperSwellDirection() int32 {
	if e.fuse == nil {
		return -1
	}
	return e.fuse.direction
}

// Creeper.tick uses the previous direction before Mob.serverAiStep ticks its goals.
func (f *creeperFuse) tick(e WorldEntity, players []SimulationPlayer, peaceful bool, canSee func(SimulationPlayer) (bool, error)) (CreeperFuseEvent, error) {
	var event CreeperFuseEvent
	f.ticks++
	old := f.direction
	if ignited, _ := e.Data.Number("ignited"); ignited != 0 {
		f.direction = 1
	}
	event.Primed = f.direction > 0 && f.swell == 0
	f.swell = max(0, f.swell+f.direction)
	limit, ok := e.Data.Number("Fuse")
	if !ok {
		limit = 30
	}
	if f.swell >= int32(int16(limit)) {
		f.swell = int32(int16(limit))
		radius, ok := e.Data.Number("ExplosionRadius")
		if !ok {
			radius = 3
		}
		event.Exploded = true
		event.Power = float32(int8(radius))
		if e.IsChargedCreeper() {
			event.Power *= 2
		}
		return event, nil
	}
	beforeAI := f.direction
	if noAI, _ := e.Data.Number("NoAI"); noAI == 0 {
		if err := f.tickGoals(e, players, peaceful, canSee); err != nil {
			return event, err
		}
	}
	// A manual ignition and the goal can both write this field in one tick.
	event.DirectionChanged = old != beforeAI || beforeAI != f.direction
	return event, nil
}

func (p SimulationPlayer) distanceSquared(pos [3]float64) float64 {
	x, y, z := p.X-pos[0], p.Y-pos[1], p.Z-pos[2]
	return x*x + y*y + z*z
}

func creeperTarget(players []SimulationPlayer, id int32) *SimulationPlayer {
	for i := range players {
		if players[i].ID == id && !players[i].Creative && !players[i].Spectator {
			return &players[i]
		}
	}
	return nil
}

func (f *creeperFuse) tickGoals(e WorldEntity, players []SimulationPlayer, peaceful bool, canSee func(SimulationPlayer) (bool, error)) error {
	if f.ticks <= 1 || (f.ticks+e.RuntimeID)&1 == 0 {
		target := creeperTarget(players, f.target)
		follow := livingAttribute(e, "minecraft:generic.follow_range", 16)
		if target != nil && !peaceful && target.distanceSquared(e.Position) <= follow*follow {
			seen, err := canSee(*target)
			if err != nil {
				return err
			}
			if seen {
				f.unseen = 0
			} else {
				f.unseen++
			}
			if f.unseen > 30 {
				target = nil
			}
		} else {
			target = nil
		}
		if target == nil {
			f.target = -1
			// NearestAttackableTargetGoal uses reducedTickDelay(10) on full AI ticks.
			if f.random.NextIntBound(5) == 0 && !peaceful {
				nearest := math.Inf(1)
				for i := range players {
					p := &players[i]
					rangeLimit := follow
					if p.Crouching {
						rangeLimit *= .8
					}
					distance := p.distanceSquared(e.Position)
					if p.Creative || p.Spectator || distance > rangeLimit*rangeLimit || distance >= nearest {
						continue
					}
					seen, err := canSee(*p)
					if err != nil {
						return err
					}
					if seen {
						target = p
						nearest = distance
					}
				}
				if target != nil {
					f.target = target.ID
					f.unseen = 0
				}
			}
		}
		canRun := f.direction > 0 || target != nil && target.distanceSquared(e.Position) < 9
		if canRun && !f.running {
			f.goalTarget = f.target
		}
		f.running = canRun
	}
	if f.running {
		f.direction = -1
		if target := creeperTarget(players, f.goalTarget); target != nil && target.distanceSquared(e.Position) <= 49 {
			seen, err := canSee(*target)
			if err != nil {
				return err
			}
			if seen {
				f.direction = 1
			}
		}
	}
	return nil
}

func (w *World) creeperSight(pos [3]float64, p SimulationPlayer) (bool, error) {
	if p.distanceSquared(pos) > 128*128 {
		return false, nil
	}
	from := pos
	from[1] += float64(float32(1.445))
	eye := float32(1.62)
	if p.Crouching {
		eye = 1.27
	}
	to := [3]float64{p.X, p.Y + float64(eye), p.Z}
	return w.clearCollisionRay(from, to)
}

func (w *World) clearCollisionRay(from, to [3]float64) (bool, error) {
	var bounds blockrules.Box
	for i := range from {
		if math.Abs(from[i]-to[i]) > 16 {
			mid := [3]float64{(from[0] + to[0]) / 2, (from[1] + to[1]) / 2, (from[2] + to[2]) / 2}
			clear, err := w.clearCollisionRay(from, mid)
			if !clear || err != nil {
				return clear, err
			}
			return w.clearCollisionRay(mid, to)
		}
		bounds[i] = min(from[i], to[i]) - 1e-6
		bounds[i+3] = max(from[i], to[i]) + 1e-6
	}
	boxes, err := w.collisionBoxes(bounds)
	if errors.Is(err, ErrChunkNotLoaded) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return clearThroughBoxes(from, to, boxes), nil
}

func clearThroughBoxes(from, to [3]float64, boxes []blockrules.Box) bool {
	for _, box := range boxes {
		enter, leave := 0., 1.
		for i := range from {
			d := to[i] - from[i]
			if d == 0 {
				if from[i] <= box[i] || from[i] >= box[i+3] {
					leave = -1
					break
				}
				continue
			}
			a, b := (box[i]-from[i])/d, (box[i+3]-from[i])/d
			enter = max(enter, min(a, b))
			leave = min(leave, max(a, b))
		}
		if enter < leave {
			return false
		}
	}
	return true
}

func (w *World) tickCreeper(key chunkKey, mob WorldEntity, peaceful bool, publish func(LivingDamageChange)) error {
	var state creeperFuse
	if mob.fuse == nil {
		state = newCreeperFuse(rand.Int64())
	} else {
		state = *mob.fuse
	}
	visible := make(map[int32]bool)
	event, err := state.tick(mob, w.simulationPlayers, peaceful, func(p SimulationPlayer) (bool, error) {
		if seen, ok := visible[p.ID]; ok {
			return seen, nil
		}
		seen, err := w.creeperSight(mob.Position, p)
		visible[p.ID] = seen
		return seen, err
	})
	if err != nil {
		return err
	}
	var change LivingDamageChange
	err = w.fallingEntityOperation(key.x, key.z, func(r *Region, ch *Chunk) error {
		for i := range ch.entities {
			e := &ch.entities[i]
			if e.UUID != mob.UUID {
				continue
			}
			// An earlier creeper in this chunk may already have killed this one.
			if hp, ok := e.Data["Health"].Value.(float32); ok && hp <= 0 {
				return nil
			}
			if !event.Exploded {
				e.fuse = &state
				if !event.DirectionChanged && !event.Primed {
					return nil
				}
				e.Revision++
			}
			change = LivingDamageChange{Entity: *e, Fuse: &event, Removed: event.Exploded}
			change.Entity.fuse = &state
			if event.Exploded {
				riders, err := EntityPassengers(*e)
				if err != nil {
					return err
				}
				for j := range riders {
					riders[j].OnFire = ch.hurtCooldowns[riders[j].UUID].burning
				}
				remaining := slices.Delete(slices.Clone(ch.entities), i, i+1)
				remaining = append(remaining, riders...)
				if w.store != nil {
					if err := persistChunkEntities(r, ch, remaining); err != nil {
						return err
					}
				} else {
					ch.entities = remaining
					ch.entitiesDirty = true
					ch.indexLivingDamage()
				}
				delete(ch.hurtCooldowns, e.UUID)
				ch.indexSkeletonTraps()
			}
			break
		}
		return nil
	})
	if err != nil || change.Fuse == nil {
		return err
	}
	if silent, _ := mob.Data.Number("Silent"); event.Primed && silent == 0 {
		change.SoundSeed = w.nextSoundSeed()
	}
	if event.Exploded {
		change.Explosion = &event.ExplosionEvent
		err = w.creeperBlast(change, publish)
	}
	if publish != nil {
		publish(change)
	}
	return err
}
