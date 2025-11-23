package pr

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/ocenb/pr-assignment/internal/errs"
	"github.com/ocenb/pr-assignment/internal/logattr"
	"github.com/ocenb/pr-assignment/internal/storage/transactor"
)

type Repo interface {
	Create(ctx context.Context, pr *api.CreatePullRequestReq, reviewers []uuid.UUID) (*api.PullRequest, error)
	GetByID(ctx context.Context, prID uuid.UUID) (*api.PullRequest, error)
	Merge(ctx context.Context, prID uuid.UUID) error
	GetTwoActiveTeamMembersByAuthorID(ctx context.Context, authorID uuid.UUID) ([]uuid.UUID, error)
	ValidatePRForReassign(ctx context.Context, prID, oldReviewerID uuid.UUID) error
	GetReassignmentCandidate(ctx context.Context, prID, oldReviewerID uuid.UUID) (uuid.UUID, error)
	ReplaceReviewer(ctx context.Context, prID, oldUserID, newUserID uuid.UUID) error
}

type Service struct {
	log  *slog.Logger
	repo Repo
	tm   *transactor.Manager
}

func New(log *slog.Logger, repo Repo, tm *transactor.Manager) *Service {
	return &Service{
		log:  log,
		repo: repo,
		tm:   tm,
	}
}

func (s *Service) Create(ctx context.Context, req *api.CreatePullRequestReq) (api.CreatePullRequestRes, error) {
	log := s.log.With(
		logattr.Op("PRService.Create"),
		slog.String("pr_id", req.PullRequestID.String()),
		slog.String("author_id", req.AuthorID.String()),
	)

	var pr *api.PullRequest
	err := s.tm.Run(ctx, func(ctxTX context.Context) error {
		reviewers, err := s.repo.GetTwoActiveTeamMembersByAuthorID(ctxTX, req.AuthorID)
		if err != nil {
			return err
		}

		pr, err = s.repo.Create(ctxTX, req, reviewers)
		return err
	})
	if err != nil {
		if errors.Is(err, errs.ErrUserNotFound) {
			return &api.CreatePullRequestNotFound{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTFOUND,
					Message: "Author not found",
				},
			}, nil
		}
		if errors.Is(err, errs.ErrPRAlreadyExists) {
			return &api.CreatePullRequestConflict{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodePREXISTS,
					Message: "Pull request already exists",
				},
			}, nil
		}

		log.Error("failed to create pull request", logattr.Err(err))
		return &api.CreatePullRequestInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	return &api.CreatePullRequestCreated{
		Pr: api.NewOptPullRequest(*pr),
	}, nil
}

func (s *Service) Merge(ctx context.Context, req *api.MergePullRequestReq) (api.MergePullRequestRes, error) {
	log := s.log.With(
		logattr.Op("PRService.Merge"),
		slog.String("pr_id", req.PullRequestID.String()),
	)

	if err := s.repo.Merge(ctx, req.PullRequestID); err != nil {
		if errors.Is(err, errs.ErrPRNotFound) {
			return &api.MergePullRequestNotFound{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTFOUND,
					Message: "Pull request not found",
				},
			}, nil
		}

		log.Error("failed to merge pull request", logattr.Err(err))
		return &api.MergePullRequestInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	pr, err := s.repo.GetByID(ctx, req.PullRequestID)
	if err != nil {
		log.Error("failed to get merged pull request", logattr.Err(err))
		return &api.MergePullRequestInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	return &api.MergePullRequestOK{
		Pr: api.NewOptPullRequest(*pr),
	}, nil
}

func (s *Service) ReassignReviewer(ctx context.Context, req *api.ReassignReviewerReq) (api.ReassignReviewerRes, error) {
	log := s.log.With(
		logattr.Op("PRService.ReassignReviewer"),
		slog.String("pr_id", req.PullRequestID.String()),
		slog.String("old_user_id", req.OldUserID.String()),
	)

	var pr *api.PullRequest
	var newReviewerID uuid.UUID

	err := s.tm.Run(ctx, func(ctxTX context.Context) error {
		if err := s.repo.ValidatePRForReassign(ctxTX, req.PullRequestID, req.OldUserID); err != nil {
			return err
		}

		var err error
		newReviewerID, err = s.repo.GetReassignmentCandidate(ctxTX, req.PullRequestID, req.OldUserID)
		if err != nil {
			return err
		}

		if err := s.repo.ReplaceReviewer(ctxTX, req.PullRequestID, req.OldUserID, newReviewerID); err != nil {
			return err
		}

		pr, err = s.repo.GetByID(ctxTX, req.PullRequestID)
		return err
	})

	if err != nil {
		if errors.Is(err, errs.ErrPRNotFound) {
			return &api.ReassignReviewerNotFound{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTFOUND,
					Message: "Pull request not found",
				},
			}, nil
		}
		if errors.Is(err, errs.ErrUserNotFound) {
			return &api.ReassignReviewerNotFound{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTFOUND,
					Message: "User not found",
				},
			}, nil
		}
		if errors.Is(err, errs.ErrPRMerged) {
			return &api.ReassignReviewerConflict{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodePRMERGED,
					Message: "Cannot reassign on merged PR",
				},
			}, nil
		}
		if errors.Is(err, errs.ErrNotAssigned) {
			return &api.ReassignReviewerConflict{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOTASSIGNED,
					Message: "Reviewer is not assigned to this PR",
				},
			}, nil
		}
		if errors.Is(err, errs.ErrNoCandidate) {
			return &api.ReassignReviewerConflict{
				Error: api.ErrorResponseError{
					Code:    api.ErrorResponseErrorCodeNOCANDIDATE,
					Message: "No active replacement candidate in team",
				},
			}, nil
		}

		log.Error("failed to reassign reviewer", logattr.Err(err))
		return &api.ReassignReviewerInternalServerError{
			Error: api.InternalError(),
		}, nil
	}

	return &api.ReassignReviewerOK{
		Pr:         *pr,
		ReplacedBy: newReviewerID,
	}, nil
}
