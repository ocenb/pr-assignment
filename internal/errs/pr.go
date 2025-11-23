package errs

import "errors"

var (
	ErrPRNotFound      = errors.New("pull request not found")
	ErrPRAlreadyExists = errors.New("pull request already exists")
	ErrPRMerged        = errors.New("pull request already merged")
	ErrNotAssigned     = errors.New("reviewer not assigned to this PR")
	ErrNoCandidate     = errors.New("no active replacement candidate")
)
