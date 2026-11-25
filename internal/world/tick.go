package world

import (
	"errors"
	"fmt"
)

// TickChunks selects which loaded chunks can run each phase. Predicates are
// read-only and cannot call chunk actors. Blocks runs under the queue lock.
// A nil predicate disables its phase for this tick.
type TickChunks struct {
	Blocks   func(x, z int32) bool // Scheduled blocks and fluids.
	Entities func(x, z int32) bool
}

// Tick advances one game tick, runs scheduled blocks and fluids, then moves
// falling entities. A failed phase leaves the clock advanced and stops the
// remaining phases for this dimension. A zero TickChunks advances time only.
func (w *World) Tick(chunks TickChunks) error {
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	return w.tickLocked(chunks, false)
}

func (w *World) tickLocked(chunks TickChunks, live bool) error {
	select {
	case <-w.done:
		return ErrWorldClosed
	default:
	}
	var failure error
	var runtime *fluidRuntime
	if chunks.Blocks != nil {
		// A saved remaining delay is relative to activation at this boundary.
		// Advancing first would add a game tick after every unload/reload.
		runtime, failure = w.prepareScheduledLocked(chunks.Blocks, live)
	}
	w.advanceTimeLocked(1)
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
	if chunks.Entities != nil {
		if err := w.tickFallingEntitiesLocked(chunks.Entities, live); err != nil {
			return errors.Join(failure, fmt.Errorf("falling entities: %w", err))
		}
	}
	return failure
}
