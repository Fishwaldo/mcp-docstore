package web

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSessionTokenUniqueAndHashed(t *testing.T) {
	raw1, hash1, err := newSessionToken()
	require.NoError(t, err)
	raw2, _, err := newSessionToken()
	require.NoError(t, err)
	require.NotEqual(t, raw1, raw2, "raw tokens must be unique")
	require.NotEqual(t, raw1, hash1, "stored value must be the hash, not the raw token")

	want := sha256.Sum256([]byte(raw1))
	require.Equal(t, hex.EncodeToString(want[:]), hash1)
	require.Equal(t, hash1, hashToken(raw1), "hashToken must reproduce the stored hash")
}
