// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	defaultCaminoValidatorWeight = 2 * units.KiloAvax
	defaultMinStakingDuration    = 24 * time.Hour
	defaultTxFee                 = uint64(100)
)

var (
	caminoPreFundedKeys      = crypto.BuildTestKeys()
	defaultGenesisTime       = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultValidateStartTime = defaultGenesisTime
	defaultValidateEndTime   = defaultValidateStartTime.Add(10 * defaultMinStakingDuration)
)

func generateTestOut(assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID) *avax.TransferableOutput {
	var out avax.TransferableOut = &secp256k1fx.TransferOutput{
		Amt:          amount,
		OutputOwners: outputOwners,
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		out = &locked.Out{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableOut: out,
		}
	}
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: assetID},
		Out:   out,
	}
}

func generateTestStakeableOut(assetID ids.ID, amount, locktime uint64, outputOwners secp256k1fx.OutputOwners) *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: assetID},
		Out: &stakeable.LockOut{
			Locktime: locktime,
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: outputOwners,
			},
		},
	}
}

func generateTestStakeableIn(assetID ids.ID, amount, locktime uint64, sigIndices []uint32) *avax.TransferableInput {
	return &avax.TransferableInput{
		Asset: avax.Asset{ID: assetID},
		In: &stakeable.LockIn{
			Locktime: locktime,
			TransferableIn: &secp256k1fx.TransferInput{
				Amt: amount,
				Input: secp256k1fx.Input{
					SigIndices: sigIndices,
				},
			},
		},
	}
}

func generateTestIn(assetID ids.ID, amount uint64, depositTxID, bondTxID ids.ID, sigIndices []uint32) *avax.TransferableInput {
	var in avax.TransferableIn = &secp256k1fx.TransferInput{
		Amt: amount,
		Input: secp256k1fx.Input{
			SigIndices: sigIndices,
		},
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		in = &locked.In{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableIn: in,
		}
	}
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
		Asset:  avax.Asset{ID: assetID},
		In:     in,
	}
}

func defaultContext() *snow.Context {
	ctx := snow.DefaultContextTest()
	ctx.AVAXAssetID = ids.ID{'C', 'A', 'M'}
	ctx.NetworkID = 5
	ctx.ChainID = ids.ID{'T', 'E', 'S', 'T'}
	return ctx
}
