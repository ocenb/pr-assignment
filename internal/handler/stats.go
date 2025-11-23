package handler

import (
	"context"

	"github.com/ocenb/pr-assignment/internal/api"
)

func (h *Handler) GetAssignmentStats(ctx context.Context) (api.GetAssignmentStatsRes, error) {
	return h.statsService.GetAssignmentStats(ctx)
}
