package stats

import (
	"context"
	"log/slog"

	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/ocenb/pr-assignment/internal/logattr"
)

type Repo interface {
	GetUserAssignmentStats(ctx context.Context) ([]api.GetAssignmentStatsOKUserStatsItem, error)
	GetPRAssignmentStats(ctx context.Context) ([]api.GetAssignmentStatsOKPrStatsItem, error)
}

type Service struct {
	log  *slog.Logger
	repo Repo
}

func New(log *slog.Logger, repo Repo) *Service {
	return &Service{
		log:  log,
		repo: repo,
	}
}

func (s *Service) GetAssignmentStats(ctx context.Context) (api.GetAssignmentStatsRes, error) {
	log := s.log.With(logattr.Op("StatsService.GetAssignmentStats"))

	userStats, err := s.repo.GetUserAssignmentStats(ctx)
	if err != nil {
		log.Error("failed to get user stats", logattr.Err(err))
		return nil, err
	}

	prStats, err := s.repo.GetPRAssignmentStats(ctx)
	if err != nil {
		log.Error("failed to get PR stats", logattr.Err(err))
		return nil, err
	}

	return &api.GetAssignmentStatsOK{
		UserStats: userStats,
		PrStats:   prStats,
	}, nil
}
