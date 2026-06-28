package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSetCookieAttributes(t *testing.T) {
	rec := httptest.NewRecorder()
	setCookie(rec, "ds_session", "val", true, time.Hour)
	cs := rec.Result().Cookies()
	require.Len(t, cs, 1)
	c := cs[0]
	require.Equal(t, "ds_session", c.Name)
	require.Equal(t, "val", c.Value)
	require.True(t, c.HttpOnly)
	require.True(t, c.Secure)
	require.Equal(t, http.SameSiteLaxMode, c.SameSite)
	require.Equal(t, "/", c.Path)
	require.Greater(t, c.MaxAge, 0)
}

func TestClearCookieExpires(t *testing.T) {
	rec := httptest.NewRecorder()
	clearCookie(rec, "ds_session", false)
	c := rec.Result().Cookies()[0]
	require.Equal(t, "", c.Value)
	require.Less(t, c.MaxAge, 0)
	require.False(t, c.Secure)
}
