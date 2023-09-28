// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txheap"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	PreFundedTestKeys         = secp256k1.TestKeys()
	DefaultGenesisTime        = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	DefaultValidateStartTime  = DefaultGenesisTime
	DefaultMinStakingDuration = 24 * time.Hour
	DefaultMaxStakingDuration = 365 * 24 * time.Hour
	DefaultValidateEndTime    = DefaultValidateStartTime.Add(10 * DefaultMinStakingDuration)

	AvaxAssetID = ids.ID{'y', 'e', 'e', 't'}
	XChainID    = ids.Empty.Prefix(0)
	CChainID    = ids.Empty.Prefix(1)

	DefaultMinValidatorStake = 5 * units.MilliAvax
	DefaultBalance           = 100 * DefaultMinValidatorStake
	DefaultWeight            = 10 * units.KiloAvax
)

func BuildTestGenesis(networkID uint32) (*State, error) {
	genesisUtxos := make([]*avax.UTXO, len(PreFundedTestKeys))
	for i, key := range PreFundedTestKeys {
		addr := key.PublicKey().Address()
		genesisUtxos[i] = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: AvaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: DefaultBalance,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
		}
	}

	vdrs := txheap.NewByEndTime()
	for _, key := range PreFundedTestKeys {
		addr := key.PublicKey().Address()
		nodeID := ids.NodeID(key.PublicKey().Address())

		utxo := &avax.TransferableOutput{
			Asset: avax.Asset{ID: AvaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: DefaultWeight,
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
				NetworkID:    networkID,
				BlockchainID: constants.PlatformChainID,
			}},
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(DefaultValidateStartTime.Unix()),
				End:    uint64(DefaultValidateEndTime.Unix()),
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

	return &State{
		GenesisBlkID:  hashing.ComputeHash256Array(ids.Empty[:]),
		UTXOs:         genesisUtxos,
		Validators:    vdrs.List(),
		Chains:        nil,
		Timestamp:     uint64(DefaultGenesisTime.Unix()),
		InitialSupply: 360 * units.MegaAvax,
	}, nil
}
