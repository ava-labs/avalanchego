// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("failed due to %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	v, err := BuildViper(os.Args[1:])
	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Printf("failed to build config: %s\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodes, err := getNodes(ctx, v)
	if err != nil {
		return fmt.Errorf("failed to get nodes: %w", err)
	}

	querier, err := newQuerierFromViper(v)
	if err != nil {
		return fmt.Errorf("failed to create querier: %w", err)
	}

	return querier.queryPeers(ctx, nodes)
}
