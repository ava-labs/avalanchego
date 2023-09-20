// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transfer

import (
	"encoding/json"
	"log"
	"time"

	"github.com/spf13/cobra"

	"github.com/ava-labs/xsvm/api"
	"github.com/ava-labs/xsvm/tx"
)

func Command() *cobra.Command {
	c := &cobra.Command{
		Use:   "transfer",
		Short: "Issues a transfer transaction",
		RunE:  transferFunc,
	}
	flags := c.Flags()
	AddFlags(flags)
	return c
}

func transferFunc(c *cobra.Command, args []string) error {
	flags := c.Flags()
	config, err := ParseFlags(flags, args)
	if err != nil {
		return err
	}

	ctx := c.Context()

	client := api.NewClient(config.URI, config.ChainID.String())

	nonce, err := client.Nonce(ctx, config.PrivateKey.Address())
	if err != nil {
		return err
	}

	utx := &tx.Transfer{
		ChainID: config.ChainID,
		Nonce:   nonce,
		MaxFee:  config.MaxFee,
		AssetID: config.AssetID,
		Amount:  config.Amount,
		To:      config.To,
	}
	stx, err := tx.Sign(utx, config.PrivateKey)
	if err != nil {
		return err
	}

	txJSON, err := json.MarshalIndent(stx, "", "  ")
	if err != nil {
		return err
	}

	issueTxStartTime := time.Now()
	txID, err := client.IssueTx(ctx, stx)
	if err != nil {
		return err
	}
	log.Printf("issued tx %s in %s\n%s\n", txID, time.Since(issueTxStartTime), string(txJSON))
	return nil
}
