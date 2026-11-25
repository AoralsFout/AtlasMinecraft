package world

const portalTicketTimeout = 300

// PlacePortalTicket is the post-transition action on the destination world.
// Native tickets use individual block positions. All portal tickets in one
// chunk have the same strength and expire only by age, so the most recent
// arrival determines that chunk's entire remaining lifetime.
func (w *World) PlacePortalTicket(pos BlockPosition) {
	center := ChunkPosition{pos.X >> 4, pos.Z >> 4}
	w.blockUpdateMu.Lock()
	defer w.blockUpdateMu.Unlock()
	select {
	case <-w.done:
		return
	default:
	}
	if w.portalTicks == nil {
		w.portalTicks = make(map[ChunkPosition]int)
	}
	_, present := w.portalTicks[center]
	w.portalTicks[center] = portalTicketTimeout
	if !present {
		w.rebuildSimulationTicketsLocked()
	}
}

func (w *World) tickPortalTicketsLocked() {
	removed := false
	for center, remaining := range w.portalTicks {
		// Ticket.timedOut uses a strict age > timeout comparison.
		if remaining == 0 {
			delete(w.portalTicks, center)
			removed = true
		} else {
			w.portalTicks[center] = remaining - 1
		}
	}
	if removed {
		w.rebuildSimulationTicketsLocked()
	}
}
