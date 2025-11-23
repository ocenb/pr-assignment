package handler

import (
	"context"
	"strings"

	"github.com/ocenb/pr-assignment/internal/api"
)

func (h *Handler) CheckHealth(ctx context.Context) (api.CheckHealthRes, error) {
	return &api.CheckHealthOK{Data: strings.NewReader("OK")}, nil
}
