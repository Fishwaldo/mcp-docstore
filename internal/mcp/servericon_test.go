package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

func TestServerAdvertisesIcons(t *testing.T) {
	svc, _, id, _ := newSvc(t)
	icons := []sdk.Icon{{Source: "https://example.com/icon-512.png", MIMEType: "image/png", Sizes: []string{"512x512"}}}
	srv := NewMCPServer(svc, func(*sdk.CallToolRequest) (store.Identity, bool) { return id, true }, nil, icons)
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	_, err := srv.Connect(ctx, st, nil)
	require.NoError(t, err)
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close() })
	info := cs.InitializeResult()
	require.NotNil(t, info.ServerInfo)
	require.Len(t, info.ServerInfo.Icons, 1)
	require.Equal(t, "https://example.com/icon-512.png", info.ServerInfo.Icons[0].Source)
}
