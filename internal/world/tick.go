package world

import "fmt"

// TickChunks selects which loaded chunks can run each phase. Predicates must
// be pure and nonblocking. A nil predicate disables its phase for this tick.
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
	select {
	case <-w.done:
		return ErrWorldClosed
	default:
	}
	w.advanceTimeLocked(1)
	if chunks.Blocks != nil {
		if err := w.tickScheduledLocked(chunks.Blocks, true); err != nil {
			return fmt.Errorf("scheduled updates: %w", err)
		}
	}
	if chunks.Entities != nil {
		if err := w.tickFallingEntitiesLocked(chunks.Entities); err != nil {
			return fmt.Errorf("falling entities: %w", err)
		}
	}
	return nil
}
