// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"net/http"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// httpStatusForError maps store/domain errors to HTTP status codes.
// ErrNotFound and ErrPermission both return 404 (existence is hidden).
// ErrConflict returns 409. ErrInvalid returns 400. All other errors return 500.
func httpStatusForError(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrPermission):
		return http.StatusNotFound
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
