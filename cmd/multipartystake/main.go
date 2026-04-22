// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

var uri string

func main() {
	rootCmd := &cobra.Command{
		Use:   "multipartystake",
		Short: "Multi-party staking tool for Avalanche P-chain",
	}
	rootCmd.PersistentFlags().StringVar(&uri, "uri", primary.LocalAPIURI, "Avalanche node URI")

	rootCmd.AddCommand(prepareCmd())
	rootCmd.AddCommand(signTxCmd())
	rootCmd.AddCommand(submitTxCmd())
	rootCmd.AddCommand(prepareSplitCmd())
	rootCmd.AddCommand(signSplitCmd())
	rootCmd.AddCommand(submitSplitCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func stripHexPrefix(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}
