// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package export

import (
	"context"
	"log"
	"time"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"
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

	txStatus, err := Export(c.Context(), config)
	if err != nil {
		return err
	}

	msg, err := txStatus.GetMessage()
	if err != nil {
		return err
	}
	log.Print(msg)

	return nil
}

func Export(ctx context.Context, config *Config) (*tx.TxIssueStatus, error) {
	client := api.NewClient(config.URI, config.SourceChainID.String())

	address := config.PrivateKey.Address()
	nonce, err := client.Nonce(ctx, address)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	issueTxStartTime := time.Now()
	txID, err := client.IssueTx(ctx, stx)
	if err != nil {
		return nil, err
	}

	err = api.WaitForAcceptance(ctx, client, address, nonce)
	if err != nil {
		return nil, err
	}

	return &tx.TxIssueStatus{
		Tx:        stx,
		TxID:      txID,
		StartTime: issueTxStartTime,
	}, nil
}
