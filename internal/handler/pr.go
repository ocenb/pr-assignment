package handler

import (
	"context"

	"github.com/ocenb/pr-assignment/internal/api"
)

func (h *Handler) CreatePullRequest(ctx context.Context, req *api.CreatePullRequestReq) (api.CreatePullRequestRes, error) {
	return h.prService.Create(ctx, req)
}

func (h *Handler) MergePullRequest(ctx context.Context, req *api.MergePullRequestReq) (api.MergePullRequestRes, error) {
	return h.prService.Merge(ctx, req)
}

func (h *Handler) ReassignReviewer(ctx context.Context, req *api.ReassignReviewerReq) (api.ReassignReviewerRes, error) {
	return h.prService.ReassignReviewer(ctx, req)
}
