package stats

import (
	"context"
	"fmt"

	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/ocenb/pr-assignment/internal/storage/transactor"
)

type Repo struct {
	tm *transactor.Manager
}

func New(tm *transactor.Manager) *Repo {
	return &Repo{tm}
}

func (r *Repo) GetUserAssignmentStats(ctx context.Context) ([]api.GetAssignmentStatsOKUserStatsItem, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		SELECT u.id, u.username, COUNT(r.pull_request_id) as assignment_count
		FROM users u
		LEFT JOIN reviewers r ON r.user_id = u.id
		GROUP BY u.id, u.username
		ORDER BY assignment_count DESC, u.username
	`

	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get user stats: %w", err)
	}
	defer rows.Close()

	var userStats []api.GetAssignmentStatsOKUserStatsItem
	for rows.Next() {
		var stat api.GetAssignmentStatsOKUserStatsItem
		if err := rows.Scan(&stat.UserID, &stat.Username, &stat.AssignmentCount); err != nil {
			return nil, fmt.Errorf("failed to scan user stat: %w", err)
		}
		userStats = append(userStats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate user stats: %w", err)
	}

	return userStats, nil
}

func (r *Repo) GetPRAssignmentStats(ctx context.Context) ([]api.GetAssignmentStatsOKPrStatsItem, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		SELECT pr.id, pr.name, COUNT(r.user_id) as reviewer_count
		FROM pull_requests pr
		LEFT JOIN reviewers r ON r.pull_request_id = pr.id
		GROUP BY pr.id, pr.name
		ORDER BY reviewer_count DESC, pr.name
	`

	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR stats: %w", err)
	}
	defer rows.Close()

	var prStats []api.GetAssignmentStatsOKPrStatsItem
	for rows.Next() {
		var stat api.GetAssignmentStatsOKPrStatsItem
		if err := rows.Scan(&stat.PullRequestID, &stat.PullRequestName, &stat.ReviewerCount); err != nil {
			return nil, fmt.Errorf("failed to scan PR stat: %w", err)
		}
		prStats = append(prStats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate PR stats: %w", err)
	}

	return prStats, nil
}
