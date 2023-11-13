// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txheap"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Shared Unit test setup utilities for a platform vm packages

var (
	// each key controls an address that has [Balance] AVAX at genesis
	Keys = secp256k1.TestKeys()

	// chain timestamp at genesis
	GenesisTime = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)

	// time that genesis validators start validating
	ValidateStartTime  = GenesisTime
	MinStakingDuration = 24 * time.Hour
	MaxStakingDuration = 365 * 24 * time.Hour

	// time that genesis validators stop validating
	ValidateEndTime = ValidateStartTime.Add(20 * MinStakingDuration)

	NetworkID = constants.UnitTestID

	// AVAX asset ID in tests
	AvaxAssetID = ids.ID{'y', 'e', 'e', 't'}
	XChainID    = ids.Empty.Prefix(0)
	CChainID    = ids.Empty.Prefix(1)

	MinValidatorStake = 5 * units.MilliAvax
	MaxValidatorStake = 500 * units.MilliAvax

	// amount all genesis validators have in defaultVM
	Balance = 100 * MinValidatorStake
	Weight  = MinValidatorStake
)

// [BuildGenesis] is a good default to build genesis for platformVM unit tests
func BuildGenesis() (*genesis.Genesis, error) {
	genesisUTXOs := make([]*genesis.UTXO, len(Keys))
	for i, key := range Keys {
		addr := key.PublicKey().Address()
		genesisUTXOs[i] = &genesis.UTXO{
			UTXO: avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty,
					OutputIndex: uint32(i),
				},
				Asset: avax.Asset{ID: AvaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: Balance,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			},
			Message: nil,
		}
	}

	vdrs := txheap.NewByEndTime()
	for _, key := range Keys {
		addr := key.PublicKey().Address()
		nodeID := ids.NodeID(key.PublicKey().Address())

		utxo := &avax.TransferableOutput{
			Asset: avax.Asset{ID: AvaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: Weight,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
		}

		owner := &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		}

		tx := &txs.Tx{Unsigned: &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    NetworkID,
				BlockchainID: constants.PlatformChainID,
			}},
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(ValidateStartTime.Unix()),
				End:    uint64(ValidateEndTime.Unix()),
				Wght:   utxo.Output().Amount(),
			},
			StakeOuts:        []*avax.TransferableOutput{utxo},
			RewardsOwner:     owner,
			DelegationShares: reward.PercentDenominator,
		}}
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return nil, err
		}

		vdrs.Add(tx)
	}

	return &genesis.Genesis{
		GenesisID:     hashing.ComputeHash256Array(ids.Empty[:]),
		UTXOs:         genesisUTXOs,
		Validators:    vdrs.List(),
		Chains:        nil,
		Timestamp:     uint64(GenesisTime.Unix()),
		InitialSupply: 360 * units.MegaAvax,
	}, nil
}
