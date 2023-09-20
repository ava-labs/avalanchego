// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package create

import (
	"log"
	"time"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	"github.com/ava-labs/xsvm"
	"github.com/ava-labs/xsvm/genesis"
)

func Command() *cobra.Command {
	c := &cobra.Command{
		Use:   "create",
		Short: "Creates a new chain",
		RunE:  createFunc,
	}
	flags := c.Flags()
	AddFlags(flags)
	return c
}

func createFunc(c *cobra.Command, args []string) error {
	flags := c.Flags()
	config, err := ParseFlags(flags, args)
	if err != nil {
		return err
	}

	ctx := c.Context()
	kc := secp256k1fx.NewKeychain(config.PrivateKey)

	// NewWalletFromURI fetches the available UTXOs owned by [kc] on the network
	// that [uri] is hosting.
	walletSyncStartTime := time.Now()
	wallet, err := primary.NewWalletWithTxs(ctx, config.URI, kc, config.SubnetID)
	if err != nil {
		return err
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	// Get the P-chain wallet
	pWallet := wallet.P()

	genesisBytes, err := genesis.Codec.Marshal(genesis.Version, &genesis.Genesis{
		Timestamp: 0,
		Allocations: []genesis.Allocation{
			{
				Address: config.Address,
				Balance: config.Balance,
			},
		},
	})
	if err != nil {
		return err
	}

	createChainStartTime := time.Now()
	createChainTxID, err := pWallet.IssueCreateChainTx(
		config.SubnetID,
		genesisBytes,
		xsvm.ID,
		nil,
		config.Name,
		common.WithContext(ctx),
	)
	if err != nil {
		return err
	}
	log.Printf("created chain %s in %s\n", createChainTxID, time.Since(createChainStartTime))
	return nil
}
