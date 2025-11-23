package handler

import (
	"context"

	"github.com/ocenb/pr-assignment/internal/api"
)

type PRService interface {
	Create(ctx context.Context, req *api.CreatePullRequestReq) (api.CreatePullRequestRes, error)
	Merge(ctx context.Context, req *api.MergePullRequestReq) (api.MergePullRequestRes, error)
	ReassignReviewer(ctx context.Context, req *api.ReassignReviewerReq) (api.ReassignReviewerRes, error)
}

type TeamService interface {
	Get(ctx context.Context, params api.GetTeamParams) (api.GetTeamRes, error)
	Create(ctx context.Context, req *api.Team) (api.CreateTeamRes, error)
	DeactivateTeamMembers(ctx context.Context, req *api.DeactivateTeamMembersReq) (api.DeactivateTeamMembersRes, error)
}

type UserService interface {
	GetReviews(ctx context.Context, params api.GetUserReviewsParams) (api.GetUserReviewsRes, error)
	SetActive(ctx context.Context, req *api.SetUserActiveReq) (api.SetUserActiveRes, error)
}

type StatsService interface {
	GetAssignmentStats(ctx context.Context) (api.GetAssignmentStatsRes, error)
}

type Handler struct {
	prService    PRService
	teamService  TeamService
	userService  UserService
	statsService StatsService
}

func New(prService PRService, teamService TeamService, userService UserService, statsService StatsService) api.Handler {
	return &Handler{
		prService,
		teamService,
		userService,
		statsService,
	}
}
