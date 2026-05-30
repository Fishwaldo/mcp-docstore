package store

import "errors"

var (
	// ErrNotFound is returned for missing rows AND for cross-tenant / no-access
	// targets, so existence is never revealed.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when an edit's base_version is stale.
	ErrConflict = errors.New("version conflict")
	// ErrPermission is returned when the caller is known but lacks the required level.
	ErrPermission = errors.New("permission denied")
	// ErrInvalid is returned for malformed input.
	ErrInvalid = errors.New("invalid input")
)
