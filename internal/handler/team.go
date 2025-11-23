package handler

import (
	"context"

	"github.com/ocenb/pr-assignment/internal/api"
)

func (h *Handler) GetTeam(ctx context.Context, params api.GetTeamParams) (api.GetTeamRes, error) {
	return h.teamService.Get(ctx, params)
}

func (h *Handler) CreateTeam(ctx context.Context, req *api.Team) (api.CreateTeamRes, error) {
	return h.teamService.Create(ctx, req)
}

func (h *Handler) DeactivateTeamMembers(ctx context.Context, req *api.DeactivateTeamMembersReq) (api.DeactivateTeamMembersRes, error) {
	return h.teamService.DeactivateTeamMembers(ctx, req)
}
