package errs

import "errors"

var (
	ErrTeamAlreadyExists = errors.New("team already exists")
	ErrTeamNotFound      = errors.New("team not found")
)
