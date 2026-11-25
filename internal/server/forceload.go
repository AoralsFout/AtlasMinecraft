package server

import (
	"atlas/internal/player"
	"atlas/internal/world"
	"fmt"
	"slices"
	"strings"
)

func forceLoadColumn(source *player.Player, args []string) (world.ChunkPosition, error) {
	coords, err := resolveCoordinates(source, []string{args[0], "0", args[1]}, true)
	if err != nil {
		return world.ChunkPosition{}, err
	}
	return world.ChunkPosition{X: int32(coords[0]) >> 4, Z: int32(coords[2]) >> 4}, nil
}

func commandForceLoad(source *player.Player, args []string, w *world.World) string {
	spec, _ := findCommand("forceload")
	usage := "Usage: " + spec.usage
	if len(args) == 0 {
		return usage
	}
	dim := w.DimensionName()
	if args[0] == "query" {
		if len(args) != 1 && len(args) != 3 {
			return usage
		}
		forced := w.ForcedChunks()
		if len(args) == 3 {
			pos, err := forceLoadColumn(source, args[1:])
			if err != nil {
				return err.Error()
			}
			status := "not marked"
			if slices.Contains(forced, pos) {
				status = "marked"
			}
			return fmt.Sprintf("Chunk [%d, %d] in %s is %s for force loading", pos.X, pos.Z, dim, status)
		}
		if len(forced) == 0 {
			return "No force loaded chunks in " + dim
		}
		chunks := make([]string, 0, len(forced))
		for _, pos := range forced {
			chunks = append(chunks, fmt.Sprintf("[%d, %d]", pos.X, pos.Z))
		}
		return fmt.Sprintf("Force loaded chunks in %s: %s", dim, strings.Join(chunks, ", "))
	}
	if len(args) == 2 && args[0] == "remove" && args[1] == "all" {
		n, err := w.ClearForcedChunks()
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("Unmarked %d force loaded chunks in %s", n, dim)
	}
	if (args[0] != "add" && args[0] != "remove") || (len(args) != 3 && len(args) != 5) {
		return usage
	}
	from, err := forceLoadColumn(source, args[1:3])
	if err != nil {
		return err.Error()
	}
	to := from
	if len(args) == 5 {
		to, err = forceLoadColumn(source, args[3:])
		if err != nil {
			return err.Error()
		}
	}
	add := args[0] == "add"
	count, err := w.SetChunksForced(from, to, add)
	if err != nil {
		return err.Error()
	}
	if count == 0 {
		return "No forced chunks changed in " + dim
	}
	verb := "Unmarked"
	if add {
		verb = "Marked"
	}
	if count == 1 {
		return fmt.Sprintf("%s 1 chunk in %s for force loading", verb, dim)
	}
	return fmt.Sprintf("%s %d chunks in %s for force loading", verb, count, dim)
}
