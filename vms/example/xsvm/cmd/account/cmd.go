// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package account

import (
	"log"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
)

func Command() *cobra.Command {
	c := &cobra.Command{
		Use:   "account",
		Short: "Displays the state of the requested account",
		RunE:  accountFunc,
	}
	flags := c.Flags()
	AddFlags(flags)
	return c
}

func accountFunc(c *cobra.Command, args []string) error {
	flags := c.Flags()
	config, err := ParseFlags(flags, args)
	if err != nil {
		return err
	}

	ctx := c.Context()

	client := api.NewClient(config.URI, config.ChainID)

	nonce, err := client.Nonce(ctx, config.Address)
	if err != nil {
		return err
	}

	balance, err := client.Balance(ctx, config.Address, config.AssetID)
	if err != nil {
		return err
	}
	log.Printf("%s has %d of %s with nonce %d\n", config.Address, balance, config.AssetID, nonce)
	return nil
}
