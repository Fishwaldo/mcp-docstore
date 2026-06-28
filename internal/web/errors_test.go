// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/stretchr/testify/require"
)

func TestHTTPStatusForError(t *testing.T) {
	require.Equal(t, http.StatusNotFound, httpStatusForError(store.ErrNotFound))
	require.Equal(t, http.StatusNotFound, httpStatusForError(store.ErrPermission))
	require.Equal(t, http.StatusConflict, httpStatusForError(store.ErrConflict))
	require.Equal(t, http.StatusBadRequest, httpStatusForError(store.ErrInvalid))
	require.Equal(t, http.StatusInternalServerError, httpStatusForError(errors.New("boom")))
}
