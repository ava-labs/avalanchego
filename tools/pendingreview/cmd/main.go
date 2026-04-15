// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/tools/pendingreview"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		err = stacktrace.Wrap(err)
		fmt.Fprintln(os.Stderr, err)
		if pendingreview.IsUsageError(err) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "serve-proxy" {
		return runServeProxy(ctx, args[1:], stdout)
	}
	return pendingreview.Run(ctx, args, stdin, stdout, stderr)
}

func runServeProxy(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("serve-proxy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	listenAddr := flags.String("addr", pendingreviewDefaultProxyListenAddr(), "loopback listen address for the pending-review proxy")

	if err := flags.Parse(args); err != nil {
		return pendingreview.UsageError(err.Error())
	}
	if flags.NArg() != 0 {
		return pendingreview.UsageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}

	return pendingreview.ServeProxy(ctx, *listenAddr, stdout)
}

func pendingreviewDefaultProxyListenAddr() string {
	return pendingreview.DefaultProxyListenAddr()
}
