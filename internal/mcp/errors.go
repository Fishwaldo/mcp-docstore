// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"errors"
	"fmt"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

// toolErr converts a store/docs error into the error a ToolHandlerFor returns. The SDK
// packs the returned error's text into an error tool result (IsError=true) so the model
// can see and self-correct. ErrConflict messages already include the current version so
// the agent can re-read and retry.
func toolErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return errors.New("not found")
	case errors.Is(err, store.ErrPermission):
		return errors.New("permission denied")
	default:
		return err // ErrConflict / ErrInvalid carry useful detail; docs errors pass through
	}
}

func errInvalidArg(field string) error { return fmt.Errorf("%w: bad %s", store.ErrInvalid, field) }
