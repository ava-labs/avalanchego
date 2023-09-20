// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package export

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
		Use:   "export",
		Short: "Issues an export transaction",
		RunE:  exportFunc,
	}
	flags := c.Flags()
	AddFlags(flags)
	return c
}

func exportFunc(c *cobra.Command, args []string) error {
	flags := c.Flags()
	config, err := ParseFlags(flags, args)
	if err != nil {
		return err
	}

	ctx := c.Context()

	client := api.NewClient(config.URI, config.SourceChainID.String())

	nonce, err := client.Nonce(ctx, config.PrivateKey.Address())
	if err != nil {
		return err
	}

	utx := &tx.Export{
		ChainID:     config.SourceChainID,
		Nonce:       nonce,
		MaxFee:      config.MaxFee,
		PeerChainID: config.DestinationChainID,
		IsReturn:    config.IsReturn,
		Amount:      config.Amount,
		To:          config.To,
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
