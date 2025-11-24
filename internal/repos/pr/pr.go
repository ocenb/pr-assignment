package pr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/ocenb/pr-assignment/internal/errs"
	"github.com/ocenb/pr-assignment/internal/storage/transactor"
)

type ReviewerReassignment struct {
	PRID        uuid.UUID
	CandidateID uuid.UUID
}

type Repo struct {
	tm *transactor.Manager
}

func New(tm *transactor.Manager) *Repo {
	return &Repo{tm}
}

func (r *Repo) Create(ctx context.Context, pr *api.CreatePullRequestReq, reviewers []uuid.UUID) (*api.PullRequest, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		INSERT INTO pull_requests (id, name, author_id)
		VALUES ($1, $2, $3)
		RETURNING created_at
	`

	var createdAt time.Time
	err := q.QueryRow(ctx, query, pr.PullRequestID, pr.PullRequestName, pr.AuthorID).Scan(&createdAt)
	if err != nil {
		var pgErr *pgconn.PgError
		// 23505 = unique_violation
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, errs.ErrPRAlreadyExists
		}
		return nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	if len(reviewers) > 0 {
		for _, reviewerID := range reviewers {
			if err := r.AddReviewer(ctx, pr.PullRequestID, reviewerID); err != nil {
				return nil, err
			}
		}
	}

	result := &api.PullRequest{
		PullRequestID:     pr.PullRequestID,
		PullRequestName:   pr.PullRequestName,
		AuthorID:          pr.AuthorID,
		Status:            api.PullRequestStatusOPEN,
		AssignedReviewers: reviewers,
		CreatedAt:         api.NewOptNilDateTime(createdAt),
	}

	if reviewers == nil {
		result.AssignedReviewers = []uuid.UUID{}
	}

	return result, nil
}

func (r *Repo) GetByID(ctx context.Context, prID uuid.UUID) (*api.PullRequest, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		SELECT 
			pr.id, 
			pr.name, 
			pr.author_id, 
			pr.status, 
			pr.created_at, 
			pr.merged_at,
			COALESCE(array_agg(rev.user_id) FILTER (WHERE rev.user_id IS NOT NULL), ARRAY[]::UUID[]) as assigned_reviewers
		FROM pull_requests pr
		LEFT JOIN reviewers rev ON rev.pull_request_id = pr.id
		WHERE pr.id = $1
		GROUP BY pr.id, pr.name, pr.author_id, pr.status, pr.created_at, pr.merged_at
	`

	var pr api.PullRequest
	var status string
	var createdAt time.Time
	var mergedAt *time.Time
	var reviewers []uuid.UUID

	err := q.QueryRow(ctx, query, prID).Scan(
		&pr.PullRequestID,
		&pr.PullRequestName,
		&pr.AuthorID,
		&status,
		&createdAt,
		&mergedAt,
		&reviewers,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrPRNotFound
		}
		return nil, fmt.Errorf("failed to get pull request: %w", err)
	}

	pr.Status = api.PullRequestStatus(status)
	pr.CreatedAt = api.NewOptNilDateTime(createdAt)
	if mergedAt != nil {
		pr.MergedAt = api.NewOptNilDateTime(*mergedAt)
	}

	if reviewers == nil {
		reviewers = []uuid.UUID{}
	}
	pr.AssignedReviewers = reviewers

	return &pr, nil
}

func (r *Repo) Merge(ctx context.Context, prID uuid.UUID) error {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		UPDATE pull_requests
		SET status = 'MERGED', merged_at = NOW()
		WHERE id = $1 AND status = 'OPEN'
		RETURNING id
	`

	var id uuid.UUID
	err := q.QueryRow(ctx, query, prID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			checkQuery := `SELECT EXISTS(SELECT 1 FROM pull_requests WHERE id = $1)`
			if err := q.QueryRow(ctx, checkQuery, prID).Scan(&exists); err != nil {
				return fmt.Errorf("failed to check PR existence: %w", err)
			}
			if !exists {
				return errs.ErrPRNotFound
			}
			return nil
		}
		return fmt.Errorf("failed to merge pull request: %w", err)
	}

	return nil
}

func (r *Repo) GetTwoActiveTeamMembersByAuthorID(ctx context.Context, authorID uuid.UUID) ([]uuid.UUID, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		SELECT u2.id
		FROM users u1
		JOIN users u2 ON u2.team_id = u1.team_id
		WHERE u1.id = $1 
			AND u2.is_active = true 
			AND u2.id != $1
		ORDER BY RANDOM()
		LIMIT 2
	`

	rows, err := q.Query(ctx, query, authorID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active members by author: %w", err)
	}
	defer rows.Close()

	var members []uuid.UUID
	for rows.Next() {
		var memberID uuid.UUID
		if err := rows.Scan(&memberID); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		members = append(members, memberID)
	}

	if len(members) == 0 {
		var exists bool
		checkQuery := `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`
		if err := q.QueryRow(ctx, checkQuery, authorID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("failed to check author existence: %w", err)
		}
		if !exists {
			return nil, errs.ErrUserNotFound
		}
	}

	return members, nil
}

// Atomic reassignment of reviewer.
//
// 1. Get PR status.
// 2. Find candidate (same team, active, not author, not old reviewer).
// 3. Check if there is an old reviewer.
// 4. Execute reassignment, only if PR is open and candidate found.
func (r *Repo) ReassignReviewer(ctx context.Context, prID, oldReviewerID uuid.UUID) (uuid.UUID, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		WITH pr_info AS (
			SELECT status, author_id 
			FROM pull_requests 
			WHERE id = $1
		),
		candidate AS (
			SELECT u.id
			FROM pr_info pi
			JOIN users author ON author.id = pi.author_id
			JOIN users u ON u.team_id = author.team_id
			WHERE u.is_active = true
				AND u.id != pi.author_id
				AND u.id != $2
				AND NOT EXISTS (
						SELECT 1 FROM reviewers r 
						WHERE r.pull_request_id = $1 AND r.user_id = u.id
				)
			ORDER BY RANDOM()
			LIMIT 1
		),
		old_reviewer_check AS (
			SELECT 1 FROM reviewers 
			WHERE pull_request_id = $1 AND user_id = $2
		),
		update_op AS (
			UPDATE reviewers
			SET user_id = (SELECT id FROM candidate)
			WHERE pull_request_id = $1
				AND user_id = $2
				AND (SELECT status FROM pr_info) = 'OPEN'
				AND EXISTS (SELECT 1 FROM candidate)
			RETURNING user_id
		)
		SELECT 
			(SELECT status FROM pr_info) as pr_status,
			(SELECT id FROM candidate) as candidate_id,
			EXISTS(SELECT 1 FROM old_reviewer_check) as old_reviewer_exists,
			(SELECT user_id FROM update_op) as new_reviewer_id
    `

	var (
		prStatus          *string
		candidateID       *uuid.UUID
		oldReviewerExists bool
		newReviewerID     *uuid.UUID
	)

	err := q.QueryRow(ctx, query, prID, oldReviewerID).Scan(
		&prStatus,
		&candidateID,
		&oldReviewerExists,
		&newReviewerID,
	)

	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to execute reassign transaction: %w", err)
	}
	if prStatus == nil {
		return uuid.Nil, errs.ErrPRNotFound
	}
	if *prStatus == "MERGED" {
		return uuid.Nil, errs.ErrPRMerged
	}
	if !oldReviewerExists {
		return uuid.Nil, errs.ErrNotAssigned
	}
	if candidateID == nil {
		return uuid.Nil, errs.ErrNoCandidate
	}
	if newReviewerID == nil {
		return uuid.Nil, fmt.Errorf("failed to replace reviewer due to unknown reason")
	}
	return *newReviewerID, nil
}

func (r *Repo) AddReviewer(ctx context.Context, prID, userID uuid.UUID) error {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		INSERT INTO reviewers (pull_request_id, user_id)
		SELECT $1, $2
		FROM users u
		WHERE u.id = $2 AND u.is_active = true
		ON CONFLICT (pull_request_id, user_id) DO NOTHING
	`

	_, err := q.Exec(ctx, query, prID, userID)
	if err != nil {
		return fmt.Errorf("failed to add reviewer: %w", err)
	}

	return nil
}

func (r *Repo) RemoveReviewerFromOpenPRs(ctx context.Context, userID uuid.UUID) error {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		DELETE FROM reviewers r
		USING pull_requests pr
		WHERE r.pull_request_id = pr.id
			AND r.user_id = $1
			AND pr.status = 'OPEN'
	`

	_, err := q.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to remove reviewer from open PRs: %w", err)
	}

	return nil
}

func (r *Repo) GetReassignmentCandidatesForReviewer(ctx context.Context, userID uuid.UUID) ([]ReviewerReassignment, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		SELECT 
			pr.id AS pull_request_id,
			candidate.user_id AS candidate_user_id
		FROM pull_requests pr
		JOIN reviewers rev ON rev.pull_request_id = pr.id
		JOIN users u1 ON u1.id = rev.user_id
		JOIN LATERAL (
			SELECT u2.id AS user_id
			FROM users u2
			WHERE u2.team_id = u1.team_id
				AND u2.is_active = true
				AND u2.id != pr.author_id
				AND u2.id != rev.user_id
				AND NOT EXISTS (
					SELECT 1 
					FROM reviewers r 
					WHERE r.pull_request_id = pr.id 
						AND r.user_id = u2.id
				)
			ORDER BY RANDOM()
			LIMIT 1
		) candidate ON true
		WHERE rev.user_id = $1 
			AND pr.status = 'OPEN'
	`

	rows, err := q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get replacement candidates: %w", err)
	}
	defer rows.Close()

	var result []ReviewerReassignment
	for rows.Next() {
		var item ReviewerReassignment
		if err := rows.Scan(&item.PRID, &item.CandidateID); err != nil {
			return nil, fmt.Errorf("failed to scan replacement candidate: %w", err)
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate replacement candidates: %w", err)
	}

	return result, nil
}

func (r *Repo) RemoveTeamReviewersFromOpenPRs(ctx context.Context, teamName string) (int64, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
    DELETE FROM reviewers r
    USING users u, teams t, pull_requests pr
    WHERE r.user_id = u.id
      AND u.team_id = t.id
      AND r.pull_request_id = pr.id
      AND t.name = $1
      AND pr.status = 'OPEN'
	`

	result, err := q.Exec(ctx, query, teamName)
	if err != nil {
		return 0, fmt.Errorf("failed to remove team reviewers: %w", err)
	}

	return result.RowsAffected(), nil
}
