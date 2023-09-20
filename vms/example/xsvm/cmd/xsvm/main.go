// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ava-labs/xsvm/cmd/account"
	"github.com/ava-labs/xsvm/cmd/chain"
	"github.com/ava-labs/xsvm/cmd/issue"
	"github.com/ava-labs/xsvm/cmd/run"
	"github.com/ava-labs/xsvm/cmd/version"
)

func init() {
	cobra.EnablePrefixMatching = true
}

func main() {
	cmd := run.Command()
	cmd.AddCommand(
		account.Command(),
		chain.Command(),
		issue.Command(),
		version.Command(),
	)
	ctx := context.Background()
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "command failed %v\n", err)
		os.Exit(1)
	}
}
