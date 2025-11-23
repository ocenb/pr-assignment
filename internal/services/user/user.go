package user

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/ocenb/pr-assignment/internal/errs"
	"github.com/ocenb/pr-assignment/internal/logattr"
	"github.com/ocenb/pr-assignment/internal/repos/pr"
	"github.com/ocenb/pr-assignment/internal/storage/transactor"
)

type UserRepo interface {
	GetUserByID(ctx context.Context, userID uuid.UUID) (*api.User, error)
	SetActive(ctx context.Context, userID uuid.UUID, isActive bool) (*api.User, error)
	GetReviews(ctx context.Context, userID uuid.UUID) ([]api.PullRequestShort, error)
}

type PRRepo interface {
	AddReviewer(ctx context.Context, prID, userID uuid.UUID) error
	RemoveReviewerFromOpenPRs(ctx context.Context, userID uuid.UUID) error
	GetReassignmentCandidatesForReviewer(ctx context.Context, userID uuid.UUID) ([]pr.ReviewerReassignment, error)
}

type Service struct {
	log      *slog.Logger
	userRepo UserRepo
	prRepo   PRRepo
	tm       *transactor.Manager
}

func New(log *slog.Logger, userRepo UserRepo, prRepo PRRepo, tm *transactor.Manager) *Service {
	return &Service{
		log:      log,
		userRepo: userRepo,
		prRepo:   prRepo,
		tm:       tm,
	}
}

func (s *Service) GetReviews(ctx context.Context, params api.GetUserReviewsParams) (api.GetUserReviewsRes, error) {
	log := s.log.With(
		logattr.Op("UserService.GetReviews"),
		slog.String("user_id", params.UserID.String()),
	)

	pullRequests, err := s.userRepo.GetReviews(ctx, params.UserID)
	if err != nil {
		if errors.Is(err, errs.ErrUserNotFound) {
			return &api.GetUserReviewsNotFound{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTFOUND,
					Message: "User not found",
				},
			}, nil
		}

		log.Error("failed to get user reviews", logattr.Err(err))
		return &api.GetUserReviewsInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	return &api.GetUserReviewsOK{
		UserID:       params.UserID,
		PullRequests: pullRequests,
	}, nil
}

func (s *Service) SetActive(ctx context.Context, req *api.SetUserActiveReq) (api.SetUserActiveRes, error) {
	log := s.log.With(
		logattr.Op("UserService.SetActive"),
		slog.String("user_id", req.UserID.String()),
		slog.Bool("is_active", req.IsActive),
	)

	var user *api.User

	err := s.tm.Run(ctx, func(ctxTX context.Context) error {
		var err error
		user, err = s.userRepo.SetActive(ctxTX, req.UserID, req.IsActive)
		if err != nil {
			return err
		}

		if !req.IsActive {
			reassignments, err := s.prRepo.GetReassignmentCandidatesForReviewer(ctxTX, req.UserID)
			if err != nil {
				return err
			}

			if err := s.prRepo.RemoveReviewerFromOpenPRs(ctxTX, req.UserID); err != nil {
				return err
			}

			for _, r := range reassignments {
				if err := s.prRepo.AddReviewer(ctxTX, r.PRID, r.CandidateID); err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		if errors.Is(err, errs.ErrUserNotFound) {
			return &api.SetUserActiveNotFound{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTFOUND,
					Message: "User not found",
				},
			}, nil
		}

		log.Error("failed to set user active", logattr.Err(err))
		return &api.SetUserActiveInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	return &api.SetUserActiveOK{
		User: api.NewOptUser(*user),
	}, nil
}
