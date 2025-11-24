package team

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/ocenb/pr-assignment/internal/errs"
	"github.com/ocenb/pr-assignment/internal/logattr"
	"github.com/ocenb/pr-assignment/internal/storage/transactor"
)

type TeamRepo interface {
	Get(ctx context.Context, teamName string) (*api.Team, error)
	Create(ctx context.Context, team *api.Team) error
	DeactivateTeamMembers(ctx context.Context, teamName string) (int64, error)
}

type PRRepo interface {
	RemoveTeamReviewersFromOpenPRs(ctx context.Context, teamName string) (int64, error)
}

type Service struct {
	log      *slog.Logger
	teamRepo TeamRepo
	prRepo   PRRepo
	tm       *transactor.Manager
}

func New(log *slog.Logger, teamRepo TeamRepo, prRepo PRRepo, tm *transactor.Manager) *Service {
	return &Service{
		log:      log,
		teamRepo: teamRepo,
		prRepo:   prRepo,
		tm:       tm,
	}
}

func (s *Service) Get(ctx context.Context, params api.GetTeamParams) (api.GetTeamRes, error) {
	log := s.log.With(
		logattr.Op("TeamService.Get"),
		slog.String("team_name", string(params.TeamName)),
	)

	team, err := s.teamRepo.Get(ctx, string(params.TeamName))
	if err != nil {
		if errors.Is(err, errs.ErrTeamNotFound) {
			return &api.GetTeamNotFound{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTFOUND,
					Message: "Team not found",
				},
			}, nil
		}

		log.Error("failed to get team", logattr.Err(err))
		return &api.GetTeamInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	return team, nil
}

func (s *Service) Create(ctx context.Context, req *api.Team) (api.CreateTeamRes, error) {
	log := s.log.With(logattr.Op("TeamService.Create"), slog.String("team_name", string(req.TeamName)))

	if err := s.tm.Run(ctx, func(ctxTX context.Context) error {
		return s.teamRepo.Create(ctxTX, req)
	}); err != nil {
		if errors.Is(err, errs.ErrTeamAlreadyExists) {
			return &api.CreateTeamConflict{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeTEAMEXISTS,
					Message: "Team already exists",
				},
			}, nil
		}

		log.Error("failed to create team", logattr.Err(err))
		return &api.CreateTeamInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	return &api.CreateTeamCreated{
		Team: api.NewOptTeam(*req),
	}, nil
}

func (s *Service) DeactivateTeamMembers(ctx context.Context, req *api.DeactivateTeamMembersReq) (api.DeactivateTeamMembersRes, error) {
	log := s.log.With(
		logattr.Op("TeamService.DeactivateTeamMembers"),
		slog.String("team_name", string(req.TeamName)),
	)

	var deactivatedCount, removedCount int64

	err := s.tm.Run(ctx, func(ctxTX context.Context) error {
		var err error
		deactivatedCount, err = s.teamRepo.DeactivateTeamMembers(ctxTX, string(req.TeamName))
		if err != nil {
			return err
		}

		removedCount, err = s.prRepo.RemoveTeamReviewersFromOpenPRs(ctxTX, string(req.TeamName))
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		if errors.Is(err, errs.ErrTeamNotFound) {
			return &api.DeactivateTeamMembersNotFound{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTFOUND,
					Message: "Team not found",
				},
			}, nil
		}

		log.Error("failed to deactivate team members", logattr.Err(err))
		return &api.DeactivateTeamMembersInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	return &api.DeactivateTeamMembersOK{
		DeactivatedCount: int(deactivatedCount),
		RemovedCount:     int(removedCount),
	}, nil
}
