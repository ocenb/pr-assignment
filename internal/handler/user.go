package handler

import (
	"context"

	"github.com/ocenb/pr-assignment/internal/api"
)

func (h *Handler) GetUserReviews(ctx context.Context, params api.GetUserReviewsParams) (api.GetUserReviewsRes, error) {
	return h.userService.GetReviews(ctx, params)
}

func (h *Handler) SetUserActive(ctx context.Context, req *api.SetUserActiveReq) (api.SetUserActiveRes, error) {
	return h.userService.SetActive(ctx, req)
}
