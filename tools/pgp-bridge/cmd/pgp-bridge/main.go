package main

import (
	"os"

	"github.com/ava-labs/pgp-bridge/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
