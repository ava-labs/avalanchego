// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	"github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func NewWallet(
	t testing.TB,
	ctx *snow.Context,
	config *config.Internal,
	state state.State,
	kc *secp256k1fx.Keychain,
	subnetIDs []ids.ID,
	validationIDs []ids.ID,
	chainIDs []ids.ID,
) wallet.Wallet {
	var (
		require = require.New(t)
		addrs   = kc.Addresses()
		utxos   = common.NewUTXOs()
	)

	pChainUTXOs, err := avax.GetAllUTXOs(state, addrs)
	require.NoError(err)

	for _, utxo := range pChainUTXOs {
		require.NoError(utxos.AddUTXO(
			context.Background(),
			constants.PlatformChainID,
			constants.PlatformChainID,
			utxo,
		))
	}

	for _, chainID := range chainIDs {
		remoteChainUTXOs, _, _, err := avax.GetAtomicUTXOs(
			ctx.SharedMemory,
			txs.Codec,
			chainID,
			addrs,
			ids.ShortEmpty,
			ids.Empty,
			math.MaxInt,
		)
		require.NoError(err)

		for _, utxo := range remoteChainUTXOs {
			require.NoError(utxos.AddUTXO(
				context.Background(),
				chainID,
				constants.PlatformChainID,
				utxo,
			))
		}
	}

	owners := make(map[ids.ID]fx.Owner, len(subnetIDs)+len(validationIDs))
	for _, subnetID := range subnetIDs {
		owner, err := state.GetSubnetOwner(subnetID)
		require.NoError(err)
		owners[subnetID] = owner
	}
	for _, validationID := range validationIDs {
		l1Validator, err := state.GetL1Validator(validationID)
		require.NoError(err)

		var owner message.PChainOwner
		_, err = txs.Codec.Unmarshal(l1Validator.DeactivationOwner, &owner)
		require.NoError(err)
		owners[validationID] = &secp256k1fx.OutputOwners{
			Threshold: owner.Threshold,
			Addrs:     owner.Addresses,
		}
	}

	builderContext := newContext(ctx, config, state)
	backend := wallet.NewBackend(
		builderContext,
		common.NewChainUTXOs(constants.PlatformChainID, utxos),
		owners,
	)
	return wallet.New(
		&client{
			backend: backend,
		},
		builder.New(
			addrs,
			builderContext,
			backend,
		),
		signer.New(
			kc,
			backend,
		),
	)
}

type client struct {
	backend wallet.Backend
}

func (c *client) IssueTx(
	tx *txs.Tx,
	options ...common.Option,
) error {
	ops := common.NewOptions(options)
	if f := ops.PostIssuanceFunc(); f != nil {
		txID := tx.ID()
		f(txID)
	}
	ctx := ops.Context()
	return c.backend.AcceptTx(ctx, tx)
}
