// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common/utxotest"
)

var (
	testKeys = secp256k1.TestKeys()

	// We hard-code [avaxAssetID] and [subnetAssetID] to make
	// ordering of UTXOs generated by [testUTXOsList] is reproducible
	avaxAssetID   = ids.Empty.Prefix(1789)
	subnetAssetID = ids.Empty.Prefix(2024)

	testContext = &builder.Context{
		NetworkID:   constants.UnitTestID,
		AVAXAssetID: avaxAssetID,
		StaticFeeConfig: fee.StaticConfig{
			TxFee:                         units.MicroAvax,
			CreateSubnetTxFee:             19 * units.MicroAvax,
			TransformSubnetTxFee:          789 * units.MicroAvax,
			CreateBlockchainTxFee:         1234 * units.MicroAvax,
			AddPrimaryNetworkValidatorFee: 19 * units.MilliAvax,
			AddPrimaryNetworkDelegatorFee: 765 * units.MilliAvax,
			AddSubnetValidatorFee:         1010 * units.MilliAvax,
			AddSubnetDelegatorFee:         9 * units.Avax,
		},
	}
)

// These tests create a tx, then verify that utxos included in the tx are
// exactly necessary to pay fees for it.

func TestBaseTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})
		backend = NewBackend(testContext, chainUTXOs, nil)

		// builder
		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr), testContext, backend)

		// data to build the transaction
		outputToMove = &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 7 * units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{utxoAddr},
				},
			},
		}
	)

	utx, err := builder.NewBaseTx([]*avax.TransferableOutput{outputToMove})
	require.NoError(err)

	// check that the output is included in the transaction
	require.Contains(utx.Outs, outputToMove)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.TxFee,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestAddSubnetValidatorTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})

		subnetID       = ids.GenerateTestID()
		subnetAuthKey  = testKeys[0]
		subnetAuthAddr = subnetAuthKey.Address()
		subnetOwner    = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{subnetAuthAddr},
		}
		subnets = map[ids.ID]*txs.Tx{
			subnetID: {
				Unsigned: &txs.CreateSubnetTx{
					Owner: subnetOwner,
				},
			},
		}

		backend = NewBackend(testContext, chainUTXOs, subnets)

		// builder
		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr, subnetAuthAddr), testContext, backend)

		// data to build the transaction
		subnetValidator = &txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(time.Now().Add(time.Hour).Unix()),
			},
			Subnet: subnetID,
		}
	)

	// build the transaction
	utx, err := builder.NewAddSubnetValidatorTx(subnetValidator)
	require.NoError(err)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.AddSubnetValidatorFee,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestRemoveSubnetValidatorTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})

		subnetID       = ids.GenerateTestID()
		subnetAuthKey  = testKeys[0]
		subnetAuthAddr = subnetAuthKey.Address()
		subnetOwner    = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{subnetAuthAddr},
		}
		subnets = map[ids.ID]*txs.Tx{
			subnetID: {
				Unsigned: &txs.CreateSubnetTx{
					Owner: subnetOwner,
				},
			},
		}

		backend = NewBackend(testContext, chainUTXOs, subnets)

		// builder
		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr, subnetAuthAddr), testContext, backend)
	)

	// build the transaction
	utx, err := builder.NewRemoveSubnetValidatorTx(
		ids.GenerateTestNodeID(),
		subnetID,
	)
	require.NoError(err)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.TxFee,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestCreateChainTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})

		subnetID       = ids.GenerateTestID()
		subnetAuthKey  = testKeys[0]
		subnetAuthAddr = subnetAuthKey.Address()
		subnetOwner    = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{subnetAuthAddr},
		}
		subnets = map[ids.ID]*txs.Tx{
			subnetID: {
				Unsigned: &txs.CreateSubnetTx{
					Owner: subnetOwner,
				},
			},
		}

		backend = NewBackend(testContext, chainUTXOs, subnets)

		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr, subnetAuthAddr), testContext, backend)

		// data to build the transaction
		genesisBytes = []byte{'a', 'b', 'c'}
		vmID         = ids.GenerateTestID()
		fxIDs        = []ids.ID{ids.GenerateTestID()}
		chainName    = "dummyChain"
	)

	// build the transaction
	utx, err := builder.NewCreateChainTx(
		subnetID,
		genesisBytes,
		vmID,
		fxIDs,
		chainName,
	)
	require.NoError(err)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.CreateBlockchainTxFee,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestCreateSubnetTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})

		subnetID       = ids.GenerateTestID()
		subnetAuthKey  = testKeys[0]
		subnetAuthAddr = subnetAuthKey.Address()
		subnetOwner    = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{subnetAuthAddr},
		}
		subnets = map[ids.ID]*txs.Tx{
			subnetID: {
				Unsigned: &txs.CreateSubnetTx{
					Owner: subnetOwner,
				},
			},
		}

		backend = NewBackend(testContext, chainUTXOs, subnets)

		// builder
		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr, subnetAuthAddr), testContext, backend)
	)

	// build the transaction
	utx, err := builder.NewCreateSubnetTx(subnetOwner)
	require.NoError(err)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.CreateSubnetTxFee,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestTransferSubnetOwnershipTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})

		subnetID       = ids.GenerateTestID()
		subnetAuthKey  = testKeys[0]
		subnetAuthAddr = subnetAuthKey.Address()
		subnetOwner    = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{subnetAuthAddr},
		}
		subnets = map[ids.ID]*txs.Tx{
			subnetID: {
				Unsigned: &txs.CreateSubnetTx{
					Owner: subnetOwner,
				},
			},
		}

		backend = NewBackend(testContext, chainUTXOs, subnets)

		// builder
		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr, subnetAuthAddr), testContext, backend)
	)

	// build the transaction
	utx, err := builder.NewTransferSubnetOwnershipTx(
		subnetID,
		subnetOwner,
	)
	require.NoError(err)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.TxFee,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestImportTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey      = testKeys[1]
		utxos         = makeTestUTXOs(utxosKey)
		sourceChainID = ids.GenerateTestID()
		importedUTXOs = utxos[:1]
		chainUTXOs    = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
			sourceChainID:             importedUTXOs,
		})

		backend = NewBackend(testContext, chainUTXOs, nil)

		// builder
		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr), testContext, backend)

		// data to build the transaction
		importKey = testKeys[0]
		importTo  = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				importKey.Address(),
			},
		}
	)

	// build the transaction
	utx, err := builder.NewImportTx(
		sourceChainID,
		importTo,
	)
	require.NoError(err)

	require.Empty(utx.Ins) // we spend the imported input (at least partially)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.TxFee,
			},
		),
		addInputAmounts(utx.Ins, utx.ImportedInputs),
	)
}

func TestExportTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})
		backend = NewBackend(testContext, chainUTXOs, nil)

		// builder
		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr), testContext, backend)

		// data to build the transaction
		subnetID        = ids.GenerateTestID()
		exportedOutputs = []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 7 * units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{utxoAddr},
				},
			},
		}}
	)

	// build the transaction
	utx, err := builder.NewExportTx(
		subnetID,
		exportedOutputs,
	)
	require.NoError(err)

	require.Equal(utx.ExportedOutputs, exportedOutputs)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs, utx.ExportedOutputs),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.TxFee,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestTransformSubnetTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})

		subnetID       = ids.GenerateTestID()
		subnetAuthKey  = testKeys[0]
		subnetAuthAddr = subnetAuthKey.Address()
		subnetOwner    = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{subnetAuthAddr},
		}
		subnets = map[ids.ID]*txs.Tx{
			subnetID: {
				Unsigned: &txs.CreateSubnetTx{
					Owner: subnetOwner,
				},
			},
		}

		backend = NewBackend(testContext, chainUTXOs, subnets)

		// builder
		utxoAddr = utxosKey.Address()
		builder  = builder.New(set.Of(utxoAddr, subnetAuthAddr), testContext, backend)

		// data to build the transaction
		initialSupply = 40 * units.MegaAvax
		maxSupply     = 100 * units.MegaAvax
	)

	// build the transaction
	utx, err := builder.NewTransformSubnetTx(
		subnetID,
		subnetAssetID,
		initialSupply,                 // initial supply
		maxSupply,                     // max supply
		reward.PercentDenominator,     // min consumption rate
		reward.PercentDenominator,     // max consumption rate
		1,                             // min validator stake
		100*units.MegaAvax,            // max validator stake
		time.Second,                   // min stake duration
		365*24*time.Hour,              // max stake duration
		0,                             // min delegation fee
		1,                             // min delegator stake
		5,                             // max validator weight factor
		.80*reward.PercentDenominator, // uptime requirement
	)
	require.NoError(err)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID:   testContext.StaticFeeConfig.TransformSubnetTxFee,
				subnetAssetID: maxSupply - initialSupply,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestAddPermissionlessValidatorTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosOffset uint64 = 2024
		utxosKey           = testKeys[1]
		utxosAddr          = utxosKey.Address()
	)
	makeUTXO := func(amount uint64) *avax.UTXO {
		utxosOffset++
		return &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset),
				OutputIndex: uint32(utxosOffset),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{utxosAddr},
					Threshold: 1,
				},
			},
		}
	}

	var (
		utxos = []*avax.UTXO{
			makeUTXO(testContext.StaticFeeConfig.AddPrimaryNetworkValidatorFee), // UTXO to pay the fee
			makeUTXO(1 * units.NanoAvax),                                        // small UTXO
			makeUTXO(9 * units.Avax),                                            // large UTXO
		}
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})
		backend = NewBackend(testContext, chainUTXOs, nil)

		// builder
		utxoAddr   = utxosKey.Address()
		rewardKey  = testKeys[0]
		rewardAddr = rewardKey.Address()
		builder    = builder.New(set.Of(utxoAddr, rewardAddr), testContext, backend)

		// data to build the transaction
		validationRewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				rewardAddr,
			},
		}
		delegationRewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				rewardAddr,
			},
		}
	)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	// build the transaction
	utx, err := builder.NewAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(time.Now().Add(time.Hour).Unix()),
				Wght:   2 * units.Avax,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		signer.NewProofOfPossession(sk),
		avaxAssetID,
		validationRewardsOwner,
		delegationRewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// check stake amount
	require.Equal(
		map[ids.ID]uint64{
			avaxAssetID: 2 * units.Avax,
		},
		addOutputAmounts(utx.StakeOuts),
	)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs, utx.StakeOuts),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.AddPrimaryNetworkValidatorFee,
			},
		),
		addInputAmounts(utx.Ins),
	)

	// Outputs should be merged if possible. For example, if there are two
	// unlocked inputs consumed for staking, this should only produce one staked
	// output.
	require.Len(utx.StakeOuts, 1)
}

func TestAddPermissionlessDelegatorTx(t *testing.T) {
	var (
		require = require.New(t)

		// backend
		utxosKey   = testKeys[1]
		utxos      = makeTestUTXOs(utxosKey)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})
		backend = NewBackend(testContext, chainUTXOs, nil)

		// builder
		utxoAddr   = utxosKey.Address()
		rewardKey  = testKeys[0]
		rewardAddr = rewardKey.Address()
		builder    = builder.New(set.Of(utxoAddr, rewardAddr), testContext, backend)

		// data to build the transaction
		rewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				rewardAddr,
			},
		}
	)

	// build the transaction
	utx, err := builder.NewAddPermissionlessDelegatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				End:    uint64(time.Now().Add(time.Hour).Unix()),
				Wght:   2 * units.Avax,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		avaxAssetID,
		rewardsOwner,
	)
	require.NoError(err)

	// check stake amount
	require.Equal(
		map[ids.ID]uint64{
			avaxAssetID: 2 * units.Avax,
		},
		addOutputAmounts(utx.StakeOuts),
	)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs, utx.StakeOuts),
			map[ids.ID]uint64{
				avaxAssetID: testContext.StaticFeeConfig.AddPrimaryNetworkDelegatorFee,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func makeTestUTXOs(utxosKey *secp256k1.PrivateKey) []*avax.UTXO {
	// Note: we avoid ids.GenerateTestNodeID here to make sure that UTXO IDs won't change
	// run by run. This simplifies checking what utxos are included in the built txs.
	const utxosOffset uint64 = 2024

	utxosAddr := utxosKey.Address()
	return []*avax.UTXO{
		{ // a small UTXO first, which should not be enough to pay fees
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset),
				OutputIndex: uint32(utxosOffset),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 2 * units.MilliAvax,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{utxosAddr},
					Threshold: 1,
				},
			},
		},
		{ // a locked, small UTXO
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset + 1),
				OutputIndex: uint32(utxosOffset + 1),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &stakeable.LockOut{
				Locktime: uint64(time.Now().Add(time.Hour).Unix()),
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt: 3 * units.MilliAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{utxosAddr},
					},
				},
			},
		},
		{ // a subnetAssetID denominated UTXO
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset + 2),
				OutputIndex: uint32(utxosOffset + 2),
			},
			Asset: avax.Asset{ID: subnetAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 99 * units.MegaAvax,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{utxosAddr},
					Threshold: 1,
				},
			},
		},
		{ // a locked, large UTXO
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset + 3),
				OutputIndex: uint32(utxosOffset + 3),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &stakeable.LockOut{
				Locktime: uint64(time.Now().Add(time.Hour).Unix()),
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt: 88 * units.Avax,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{utxosAddr},
					},
				},
			},
		},
		{ // a large UTXO last, which should be enough to pay any fee by itself
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset + 4),
				OutputIndex: uint32(utxosOffset + 4),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 9 * units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{utxosAddr},
					Threshold: 1,
				},
			},
		},
	}
}

func addAmounts(allAmounts ...map[ids.ID]uint64) map[ids.ID]uint64 {
	amounts := make(map[ids.ID]uint64)
	for _, amountsToAdd := range allAmounts {
		for assetID, amount := range amountsToAdd {
			amounts[assetID] += amount
		}
	}
	return amounts
}

func addInputAmounts(inputSlices ...[]*avax.TransferableInput) map[ids.ID]uint64 {
	consumed := make(map[ids.ID]uint64)
	for _, inputs := range inputSlices {
		for _, in := range inputs {
			consumed[in.AssetID()] += in.In.Amount()
		}
	}
	return consumed
}

func addOutputAmounts(outputSlices ...[]*avax.TransferableOutput) map[ids.ID]uint64 {
	produced := make(map[ids.ID]uint64)
	for _, outputs := range outputSlices {
		for _, out := range outputs {
			produced[out.AssetID()] += out.Out.Amount()
		}
	}
	return produced
}
