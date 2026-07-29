package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"cyber-code/internal/cli"
	"cyber-code/internal/product"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.ExecuteWithOptions(
		ctx, os.Stdin, os.Stdout, os.Stderr, os.Args[1:],
		cli.ExecuteOptions{
			Version: product.BuildVersion, UpdateMetadataURL: product.UpdateMetadataURL,
			UpdatePublicKeyB64: product.UpdatePublicKeyB64,
		},
	))
}
