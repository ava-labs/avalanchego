// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesistest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	avalanchegenesis "github.com/ava-labs/avalanchego/genesis"
	platformvmgenesis "github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

var (
	AVAXAssetID = ids.GenerateTestID()
	AVAXAsset   = avax.Asset{ID: AVAXAssetID}

	ValidatorNodeID                  = ids.GenerateTestNodeID()
	Time                             = time.Now().Round(time.Second)
	TimeUnix                         = uint64(Time.Unix())
	ValidatorDuration                = 28 * 24 * time.Hour
	ValidatorEndTime                 = Time.Add(ValidatorDuration)
	ValidatorEndTimeUnix             = uint64(ValidatorEndTime.Unix())
	ValidatorWeight                  = units.MegaAvax
	ValidatorRewardsOwner            = &secp256k1fx.OutputOwners{}
	ValidatorDelegationShares uint32 = reward.PercentDenominator

	XChainName = "x"

	InitialBalance = 30 * units.MegaAvax
	InitialSupply  = ValidatorWeight + InitialBalance
)

func New(t testing.TB) *platformvmgenesis.Genesis {
	require := require.New(t)

	genesisValidator := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: constants.PlatformChainID,
		}},
		Validator: txs.Validator{
			NodeID: ValidatorNodeID,
			Start:  TimeUnix,
			End:    ValidatorEndTimeUnix,
			Wght:   ValidatorWeight,
		},
		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: AVAXAsset,
				Out: &secp256k1fx.TransferOutput{
					Amt: ValidatorWeight,
				},
			},
		},
		RewardsOwner:     ValidatorRewardsOwner,
		DelegationShares: ValidatorDelegationShares,
	}
	genesisValidatorTx := &txs.Tx{Unsigned: genesisValidator}
	require.NoError(genesisValidatorTx.Initialize(txs.Codec))

	genesisChain := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: constants.PlatformChainID,
		}},
		SubnetID:   constants.PrimaryNetworkID,
		ChainName:  XChainName,
		VMID:       constants.AVMID,
		SubnetAuth: &secp256k1fx.Input{},
	}
	genesisChainTx := &txs.Tx{Unsigned: genesisChain}
	require.NoError(genesisChainTx.Initialize(txs.Codec))

	return &platformvmgenesis.Genesis{
		UTXOs: []*platformvmgenesis.UTXO{
			{
				UTXO: avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        AVAXAssetID,
						OutputIndex: 0,
					},
					Asset: AVAXAsset,
					Out: &secp256k1fx.TransferOutput{
						Amt: InitialBalance,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs: []ids.ShortID{
								avalanchegenesis.EWOQKey.Address(),
							},
						},
					},
				},
				Message: nil,
			},
		},
		Validators: []*txs.Tx{
			genesisValidatorTx,
		},
		Chains: []*txs.Tx{
			genesisChainTx,
		},
		Timestamp:     TimeUnix,
		InitialSupply: InitialSupply,
	}
}

func NewBytes(t testing.TB) []byte {
	g := New(t)
	genesisBytes, err := platformvmgenesis.Codec.Marshal(platformvmgenesis.CodecVersion, g)
	require.NoError(t, err)
	return genesisBytes
}
