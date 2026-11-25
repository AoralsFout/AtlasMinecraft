package world

import (
	"errors"
	"fmt"
)

// TickChunks selects which loaded chunks can run each phase. Predicates are
// read-only and cannot call chunk actors. Blocks runs under the queue lock.
// A nil predicate disables its phase for this tick.
type TickChunks struct {
	Blocks         func(x, z int32) bool // Scheduled blocks and fluids.
	Random         func(x, z int32) bool // Random block and fluid ticks.
	publishWeather WeatherPublisher
	Entities       func(x, z int32) bool
}

// Tick advances one game tick, runs scheduled blocks, fluids and random ticks, then moves
// falling entities. A failed phase leaves the clock advanced and stops the
// remaining phases for this dimension. A zero TickChunks advances time and weather.
func (w *World) Tick(chunks TickChunks) error {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	return w.tickLocked(chunks, false, false)
}

func (w *World) tickLocked(chunks TickChunks, live, serverTick bool) error {
	select {
	case <-w.done:
		// The remaining dimensions still run on this server tick even if the
		// Overworld's simulation has already closed.
		if serverTick && w.clockSource.Load() == nil {
			w.advanceClock(1)
		}
		return ErrWorldClosed
	default:
	}
	if err := w.advanceWeather(chunks.publishWeather); err != nil {
		return err
	}
	var failure error
	var runtime *fluidRuntime
	if chunks.Blocks != nil {
		// A saved remaining delay is relative to activation at this boundary.
		// Advancing first would add a game tick after every unload/reload.
		runtime, failure = w.prepareScheduledLocked(chunks.Blocks, live)
	}
	if serverTick && w.clockSource.Load() != nil {
		age, _ := w.Time()
		w.scheduledTicks.setTime(age)
	} else {
		w.advanceTimeLocked(1)
	}
	if chunks.Blocks != nil {
		if runtime != nil && (failure == nil || live) {
			failure = errors.Join(failure, runtime.tickScheduledQueues(chunks.Blocks, true, live))
		}
		if failure != nil {
			failure = fmt.Errorf("scheduled updates: %w", failure)
			if !live {
				return failure
			}
		}
	}
	// ServerChunkCache purges tickets after scheduled blocks/fluids and
	// before ticking chunks and entities. Those later phases read live tickets.
	if live {
		w.tickPortalTicketsLocked()
	}
	if chunks.Random != nil {
		if runtime == nil {
			runtime = &fluidRuntime{world: w, settings: w.fluidSettings()}
		}
		if err := runtime.tickRandomChunks(chunks); err != nil {
			failure = errors.Join(failure, fmt.Errorf("random ticks: %w", err))
			if !live {
				return failure
			}
		}
	}
	if chunks.Entities != nil {
		if err := w.tickFallingEntitiesLocked(chunks.Entities, live); err != nil {
			return errors.Join(failure, fmt.Errorf("falling entities: %w", err))
		}
	}
	return failure
}
