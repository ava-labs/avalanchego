// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
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

	// many UTs add a subnet right after genesis. They should use
	// SubnetControlKeys to control the subnet
	SubnetControlKeys = Keys[0:3]

	// Node IDs of genesis validators. Initialized in init function
	GenesisNodeIDs []ids.NodeID

	// chain timestamp at genesis
	GenesisTime = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)

	// time that genesis validators start validating
	ValidateStartTime  = GenesisTime
	MinStakingDuration = 24 * time.Hour
	MaxStakingDuration = 365 * 24 * time.Hour

	// time that genesis validators stop validating
	ValidateEndTime = ValidateStartTime.Add(20 * MinStakingDuration)

	MinValidatorStake = 5 * units.MilliAvax
	MaxValidatorStake = 500 * units.MilliAvax

	GenesisUTXOBalance     = 100 * MinValidatorStake // amount in each genesis utxos
	GenesisValidatorWeight = MinValidatorStake       // weight of each genesis validator
)

func init() {
	for _, key := range Keys {
		// TODO: use ids.GenerateTestNodeID() instead of ids.BuildTestNodeID
		// Can be done when TestGetState is refactored
		nodeBytes := key.PublicKey().Address()
		nodeID := ids.BuildTestNodeID(nodeBytes[:])

		GenesisNodeIDs = append(GenesisNodeIDs, nodeID)
	}
}

// [BuildGenesis] is a good default to build genesis for platformVM unit tests
func BuildGenesis(ctx *snow.Context) (*genesis.Genesis, error) {
	genesisUTXOs := make([]*genesis.UTXO, len(Keys))
	for i, key := range Keys {
		addr := key.PublicKey().Address()
		genesisUTXOs[i] = &genesis.UTXO{
			UTXO: avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        ids.Empty,
					OutputIndex: uint32(i),
				},
				Asset: avax.Asset{ID: ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: GenesisUTXOBalance,
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
			Asset: avax.Asset{ID: ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: GenesisValidatorWeight,
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
				NetworkID:    ctx.NetworkID,
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
		GenesisBytes:  []byte{'g', 'e', 'n', 'e', 's', 'i', 's', 'B', 'y', 't', 'e', 's'},
		UTXOs:         genesisUTXOs,
		Validators:    vdrs.List(),
		Chains:        nil,
		Timestamp:     uint64(GenesisTime.Unix()),
		InitialSupply: 360 * units.MegaAvax,
	}, nil
}
