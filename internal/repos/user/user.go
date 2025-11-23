package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/ocenb/pr-assignment/internal/errs"
	"github.com/ocenb/pr-assignment/internal/storage/transactor"
)

type Repo struct {
	tm *transactor.Manager
}

func New(tm *transactor.Manager) *Repo {
	return &Repo{tm}
}

func (r *Repo) GetUserByID(ctx context.Context, userID uuid.UUID) (*api.User, error) {
	q := r.tm.GetQueryEngine(ctx)
	query := `
		SELECT u.id, u.username, t.name, u.is_active 
		FROM users u 
		JOIN teams t ON u.team_id = t.id 
		WHERE u.id = $1
	`

	var user api.User
	err := q.QueryRow(ctx, query, userID).Scan(&user.UserID, &user.Username, &user.TeamName, &user.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

func (r *Repo) SetActive(ctx context.Context, userID uuid.UUID, isActive bool) (*api.User, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		WITH updated AS (
			UPDATE users 
			SET is_active = $2 
			WHERE id = $1 
			RETURNING id, username, team_id, is_active
		)
		SELECT u.id, u.username, t.name, u.is_active 
		FROM updated u 
		JOIN teams t ON t.id = u.team_id
	`

	var user api.User
	err := q.QueryRow(ctx, query, userID, isActive).Scan(
		&user.UserID,
		&user.Username,
		&user.TeamName,
		&user.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	return &user, nil
}

func (r *Repo) GetReviews(ctx context.Context, userID uuid.UUID) ([]api.PullRequestShort, error) {
	q := r.tm.GetQueryEngine(ctx)

	var exists bool
	checkQuery := `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`
	if err := q.QueryRow(ctx, checkQuery, userID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return nil, errs.ErrUserNotFound
	}

	query := `
		SELECT pr.id, pr.name, pr.author_id, pr.status
		FROM pull_requests pr
		INNER JOIN reviewers rev ON rev.pull_request_id = pr.id
		WHERE rev.user_id = $1
		ORDER BY pr.created_at DESC
	`

	rows, err := q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user reviews: %w", err)
	}
	defer rows.Close()

	var pullRequests []api.PullRequestShort
	for rows.Next() {
		var pr api.PullRequestShort
		var status string

		if err := rows.Scan(&pr.PullRequestID, &pr.PullRequestName, &pr.AuthorID, &status); err != nil {
			return nil, fmt.Errorf("failed to scan pull request: %w", err)
		}

		pr.Status = api.PullRequestShortStatus(status)
		pullRequests = append(pullRequests, pr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	if pullRequests == nil {
		pullRequests = []api.PullRequestShort{}
	}

	return pullRequests, nil
}
