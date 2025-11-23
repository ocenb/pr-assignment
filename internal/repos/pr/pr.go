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
			insertReviewer := `INSERT INTO reviewers (pull_request_id, user_id) VALUES ($1, $2)`
			if _, err := q.Exec(ctx, insertReviewer, pr.PullRequestID, reviewerID); err != nil {
				return nil, fmt.Errorf("failed to add reviewer: %w", err)
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

func (r *Repo) GetReassignmentCandidate(ctx context.Context, prID, oldReviewerID uuid.UUID) (uuid.UUID, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		SELECT u.id
		FROM pull_requests pr
		JOIN users author ON author.id = pr.author_id
		JOIN users u ON u.team_id = author.team_id
		WHERE pr.id = $1
			AND u.is_active = true
			AND u.id != pr.author_id
			AND u.id != $2
			AND NOT EXISTS (
				SELECT 1
				FROM reviewers r
				WHERE r.pull_request_id = $1
				AND r.user_id = u.id
			)
		ORDER BY RANDOM()
		LIMIT 1
	`

	var candidateID uuid.UUID
	err := q.QueryRow(ctx, query, prID, oldReviewerID).Scan(&candidateID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, errs.ErrNoCandidate
		}
		return uuid.Nil, fmt.Errorf("failed to get reassignment candidate: %w", err)
	}

	return candidateID, nil
}

func (r *Repo) ValidatePRForReassign(ctx context.Context, prID, oldReviewerID uuid.UUID) error {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		SELECT 
			pr.status,
			EXISTS(SELECT 1 FROM reviewers WHERE pull_request_id = pr.id AND user_id = $2) as is_assigned
		FROM pull_requests pr
		WHERE pr.id = $1
	`

	var status string
	var isAssigned bool

	err := q.QueryRow(ctx, query, prID, oldReviewerID).Scan(&status, &isAssigned)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrPRNotFound
		}
		return fmt.Errorf("failed to validate PR for reassign: %w", err)
	}

	if status == "MERGED" {
		return errs.ErrPRMerged
	}

	if !isAssigned {
		return errs.ErrNotAssigned
	}

	return nil
}

func (r *Repo) ReplaceReviewer(ctx context.Context, prID, oldUserID, newUserID uuid.UUID) error {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		UPDATE reviewers
		SET user_id = $3
		WHERE pull_request_id = $1 AND user_id = $2
	`

	result, err := q.Exec(ctx, query, prID, oldUserID, newUserID)
	if err != nil {
		return fmt.Errorf("failed to replace reviewer: %w", err)
	}

	if result.RowsAffected() == 0 {
		return errs.ErrNotAssigned
	}

	return nil
}

func (r *Repo) AddReviewer(ctx context.Context, prID, userID uuid.UUID) error {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		INSERT INTO reviewers (pull_request_id, user_id)
		VALUES ($1, $2)
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
		DELETE FROM reviewers
		WHERE (pull_request_id, user_id) IN (
			SELECT rev.pull_request_id, rev.user_id
			FROM reviewers rev
			JOIN pull_requests pr ON pr.id = rev.pull_request_id
			JOIN users u ON u.id = rev.user_id
			JOIN teams t ON t.id = u.team_id
			WHERE t.name = $1 
				AND pr.status = 'OPEN'
		)
	`

	result, err := q.Exec(ctx, query, teamName)
	if err != nil {
		return 0, fmt.Errorf("failed to remove team reviewers: %w", err)
	}

	return result.RowsAffected(), nil
}
