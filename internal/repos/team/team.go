package team

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func (r *Repo) Get(ctx context.Context, teamName string) (*api.Team, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		SELECT t.name, u.id, u.username, u.is_active
		FROM teams t
		LEFT JOIN users u ON u.team_id = t.id
		WHERE t.name = $1
	`

	rows, err := q.Query(ctx, query, teamName)
	if err != nil {
		return nil, fmt.Errorf("failed to get team: %w", err)
	}
	defer rows.Close()

	var (
		team    api.Team
		found   bool
		members []api.TeamMember
	)

	for rows.Next() {
		found = true

		var (
			tName   string
			uID     *uuid.UUID
			uName   *string
			uActive *bool
		)

		if err := rows.Scan(&tName, &uID, &uName, &uActive); err != nil {
			return nil, fmt.Errorf("failed to scan team row: %w", err)
		}

		if uID != nil {
			members = append(members, api.TeamMember{
				UserID:   *uID,
				Username: api.Username(*uName),
				IsActive: *uActive,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	if !found {
		return nil, errs.ErrTeamNotFound
	}

	if members == nil {
		members = []api.TeamMember{}
	}
	team.Members = members
	team.TeamName = api.TeamName(teamName)

	return &team, nil
}

func (r *Repo) Create(ctx context.Context, team *api.Team) error {
	q := r.tm.GetQueryEngine(ctx)

	var teamID uuid.UUID
	queryTeam := `INSERT INTO teams (name) VALUES ($1) RETURNING id`

	err := q.QueryRow(ctx, queryTeam, team.TeamName).Scan(&teamID)
	if err != nil {
		var pgErr *pgconn.PgError
		// 23505 = unique_violation
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errs.ErrTeamAlreadyExists
		}
		return fmt.Errorf("failed to create team: %w", err)
	}

	batch := &pgx.Batch{}
	queryUser := `
		INSERT INTO users (id, username, team_id, is_active)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			username = EXCLUDED.username,
			team_id = EXCLUDED.team_id,
			is_active = EXCLUDED.is_active
	`

	for _, member := range team.Members {
		batch.Queue(queryUser, member.UserID, member.Username, teamID, member.IsActive)
	}

	br := q.SendBatch(ctx, batch)

	for range team.Members {
		_, err := br.Exec()
		if err != nil {
			closeErr := br.Close()
			return errors.Join(
				fmt.Errorf("failed to upsert user: %w", err),
				fmt.Errorf("failed to close batch: %w", closeErr),
			)
		}
	}

	if err := br.Close(); err != nil {
		return fmt.Errorf("failed to close batch: %w", err)
	}
	return nil
}

func (r *Repo) DeactivateTeamMembers(ctx context.Context, teamName string) (int64, error) {
	q := r.tm.GetQueryEngine(ctx)

	query := `
		UPDATE users u
		SET is_active = false 
		FROM teams t
		WHERE u.team_id = t.id 
			AND t.name = $1
			AND u.is_active = true
	`

	result, err := q.Exec(ctx, query, teamName)
	if err != nil {
		return 0, fmt.Errorf("failed to deactivate team members: %w", err)
	}

	rowsAffected := result.RowsAffected()

	if rowsAffected == 0 {
		var exists bool
		checkQuery := `SELECT EXISTS(SELECT 1 FROM teams WHERE name = $1)`
		if err := q.QueryRow(ctx, checkQuery, teamName).Scan(&exists); err != nil {
			return 0, fmt.Errorf("failed to check team existence: %w", err)
		}
		if !exists {
			return 0, errs.ErrTeamNotFound
		}
	}

	return rowsAffected, nil
}
