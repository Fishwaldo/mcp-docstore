package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Fishwaldo/mcp-docstore/cmd/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := server.Run(context.Background(), os.Args[1:], logger); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
