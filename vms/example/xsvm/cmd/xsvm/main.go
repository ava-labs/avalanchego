// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MetalBlockchain/metalgo/vms/example/xsvm/cmd/account"
	"github.com/MetalBlockchain/metalgo/vms/example/xsvm/cmd/chain"
	"github.com/MetalBlockchain/metalgo/vms/example/xsvm/cmd/issue"
	"github.com/MetalBlockchain/metalgo/vms/example/xsvm/cmd/run"
	"github.com/MetalBlockchain/metalgo/vms/example/xsvm/cmd/version"
	"github.com/MetalBlockchain/metalgo/vms/example/xsvm/cmd/versionjson"
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
		versionjson.Command(),
	)
	ctx := context.Background()
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "command failed %v\n", err)
		os.Exit(1)
	}
}
