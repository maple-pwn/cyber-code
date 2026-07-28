package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"claude-code-go/internal/cli"
)

const version = "2.1.88"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.ExecuteWithOptions(
		ctx, os.Stdin, os.Stdout, os.Stderr, os.Args[1:],
		cli.ExecuteOptions{Version: version},
	))
}
