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
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common/utxotest"

	feecomponent "github.com/ava-labs/avalanchego/vms/components/fee"
	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

var (
	subnetID = ids.GenerateTestID()
	nodeID   = ids.GenerateTestNodeID()

	testKeys       = secp256k1.TestKeys()
	subnetAuthKey  = testKeys[0]
	subnetAuthAddr = subnetAuthKey.Address()
	subnetOwner    = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{subnetAuthAddr},
	}
	importKey   = testKeys[0]
	importAddr  = importKey.Address()
	importOwner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{importAddr},
	}
	rewardKey    = testKeys[0]
	rewardAddr   = rewardKey.Address()
	rewardsOwner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{rewardAddr},
	}
	utxoKey   = testKeys[1]
	utxoAddr  = utxoKey.Address()
	utxoOwner = secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{utxoAddr},
	}

	// We hard-code [avaxAssetID] and [subnetAssetID] to make ordering of UTXOs
	// generated by [makeTestUTXOs] reproducible.
	avaxAssetID   = ids.Empty.Prefix(1789)
	subnetAssetID = ids.Empty.Prefix(2024)
	utxos         = makeTestUTXOs(utxoKey)

	avaxOutput = &avax.TransferableOutput{
		Asset: avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          7 * units.Avax,
			OutputOwners: utxoOwner,
		},
	}

	subnets = map[ids.ID]*txs.Tx{
		subnetID: {
			Unsigned: &txs.CreateSubnetTx{
				Owner: subnetOwner,
			},
		},
	}

	primaryNetworkPermissionlessStaker = &txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			End:    uint64(time.Now().Add(time.Hour).Unix()),
			Wght:   2 * units.Avax,
		},
		Subnet: constants.PrimaryNetworkID,
	}

	testContextPreEtna = &builder.Context{
		NetworkID:   constants.UnitTestID,
		AVAXAssetID: avaxAssetID,
		StaticFeeConfig: txfee.StaticConfig{
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
	testContextPostEtna = &builder.Context{
		NetworkID:   constants.UnitTestID,
		AVAXAssetID: avaxAssetID,

		ComplexityWeights: feecomponent.Dimensions{
			feecomponent.Bandwidth: 1,
			feecomponent.DBRead:    10,
			feecomponent.DBWrite:   100,
			feecomponent.Compute:   1000,
		},
		GasPrice: 1,
	}

	testEnvironment = []struct {
		name          string
		context       *builder.Context
		feeCalculator txfee.Calculator
	}{
		{
			name:    "Pre-Etna",
			context: testContextPreEtna,
			feeCalculator: txfee.NewStaticCalculator(
				testContextPreEtna.StaticFeeConfig,
			),
		},
		{
			name:    "Post-Etna",
			context: testContextPostEtna,
			feeCalculator: txfee.NewDynamicCalculator(
				testContextPostEtna.ComplexityWeights,
				testContextPostEtna.GasPrice,
			),
		},
	}
)

// These tests create a tx, then verify that utxos included in the tx are
// exactly necessary to pay fees for it.

func TestBaseTx(t *testing.T) {
	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, nil)
				builder = builder.New(set.Of(utxoAddr), e.context, backend)
			)

			utx, err := builder.NewBaseTx([]*avax.TransferableOutput{avaxOutput})
			require.NoError(err)
			require.Contains(utx.Outs, avaxOutput)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

func TestAddSubnetValidatorTx(t *testing.T) {
	subnetValidator := &txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			End:    uint64(time.Now().Add(time.Hour).Unix()),
		},
		Subnet: subnetID,
	}

	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, subnets)
				builder = builder.New(set.Of(utxoAddr, subnetAuthAddr), e.context, backend)
			)

			// build the transaction
			utx, err := builder.NewAddSubnetValidatorTx(subnetValidator)
			require.NoError(err)
			require.Equal(utx.SubnetValidator, *subnetValidator)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

func TestRemoveSubnetValidatorTx(t *testing.T) {
	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, subnets)
				builder = builder.New(set.Of(utxoAddr, subnetAuthAddr), e.context, backend)
			)

			// build the transaction
			utx, err := builder.NewRemoveSubnetValidatorTx(
				nodeID,
				subnetID,
			)
			require.NoError(err)
			require.Equal(utx.NodeID, nodeID)
			require.Equal(utx.Subnet, subnetID)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

func TestCreateChainTx(t *testing.T) {
	var (
		genesisBytes = []byte{'a', 'b', 'c'}
		vmID         = ids.GenerateTestID()
		fxIDs        = []ids.ID{ids.GenerateTestID()}
		chainName    = "dummyChain"
	)

	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, subnets)
				builder = builder.New(set.Of(utxoAddr, subnetAuthAddr), e.context, backend)
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
			require.Equal(utx.SubnetID, subnetID)
			require.Equal(utx.ChainName, chainName)
			require.Equal(utx.VMID, vmID)
			require.ElementsMatch(utx.FxIDs, fxIDs)
			require.Equal(utx.GenesisData, genesisBytes)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

func TestCreateSubnetTx(t *testing.T) {
	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, subnets)
				builder = builder.New(set.Of(utxoAddr, subnetAuthAddr), e.context, backend)
			)

			// build the transaction
			utx, err := builder.NewCreateSubnetTx(subnetOwner)
			require.NoError(err)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

func TestTransferSubnetOwnershipTx(t *testing.T) {
	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, subnets)
				builder = builder.New(set.Of(utxoAddr, subnetAuthAddr), e.context, backend)
			)

			// build the transaction
			utx, err := builder.NewTransferSubnetOwnershipTx(
				subnetID,
				subnetOwner,
			)
			require.NoError(err)
			require.Equal(utx.Subnet, subnetID)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

func TestImportTx(t *testing.T) {
	var (
		sourceChainID = ids.GenerateTestID()
		importedUTXOs = utxos[:1]
	)

	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
					sourceChainID:             importedUTXOs,
				})
				backend = NewBackend(e.context, chainUTXOs, nil)
				builder = builder.New(set.Of(utxoAddr), e.context, backend)
			)

			// build the transaction
			utx, err := builder.NewImportTx(
				sourceChainID,
				importOwner,
			)
			require.NoError(err)
			require.Empty(utx.Ins)                              // The imported input should be sufficient for fees
			require.Len(utx.ImportedInputs, len(importedUTXOs)) // All utxos should be imported

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins, utx.ImportedInputs),
			)
		})
	}
}

func TestExportTx(t *testing.T) {
	exportedOutputs := []*avax.TransferableOutput{avaxOutput}

	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, nil)
				builder = builder.New(set.Of(utxoAddr), e.context, backend)
			)

			// build the transaction
			utx, err := builder.NewExportTx(
				subnetID,
				exportedOutputs,
			)
			require.NoError(err)
			require.ElementsMatch(utx.ExportedOutputs, exportedOutputs)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs, utx.ExportedOutputs),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

// TransformSubnetTx is not valid to be issued post-Etna
func TestTransformSubnetTx(t *testing.T) {
	var (
		initialSupply                   = 40 * units.MegaAvax
		maxSupply                       = 100 * units.MegaAvax
		minConsumptionRate       uint64 = reward.PercentDenominator
		maxConsumptionRate       uint64 = reward.PercentDenominator
		minValidatorStake        uint64 = 1
		maxValidatorStake               = 100 * units.MegaAvax
		minStakeDuration                = time.Second
		maxStakeDuration                = 365 * 24 * time.Hour
		minDelegationFee         uint32 = 0
		minDelegatorStake        uint64 = 1
		maxValidatorWeightFactor byte   = 5
		uptimeRequirement        uint32 = .80 * reward.PercentDenominator

		require    = require.New(t)
		chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
			constants.PlatformChainID: utxos,
		})
		backend = NewBackend(testContextPreEtna, chainUTXOs, subnets)
		builder = builder.New(set.Of(utxoAddr, subnetAuthAddr), testContextPreEtna, backend)
	)

	// build the transaction
	utx, err := builder.NewTransformSubnetTx(
		subnetID,
		subnetAssetID,
		initialSupply,
		maxSupply,
		minConsumptionRate,
		maxConsumptionRate,
		minValidatorStake,
		maxValidatorStake,
		minStakeDuration,
		maxStakeDuration,
		minDelegationFee,
		minDelegatorStake,
		maxValidatorWeightFactor,
		uptimeRequirement,
	)
	require.NoError(err)
	require.Equal(utx.Subnet, subnetID)
	require.Equal(utx.AssetID, subnetAssetID)
	require.Equal(utx.InitialSupply, initialSupply)
	require.Equal(utx.MaximumSupply, maxSupply)
	require.Equal(utx.MinConsumptionRate, minConsumptionRate)
	require.Equal(utx.MinValidatorStake, minValidatorStake)
	require.Equal(utx.MaxValidatorStake, maxValidatorStake)
	require.Equal(utx.MinStakeDuration, uint32(minStakeDuration/time.Second))
	require.Equal(utx.MaxStakeDuration, uint32(maxStakeDuration/time.Second))
	require.Equal(utx.MinDelegationFee, minDelegationFee)
	require.Equal(utx.MinDelegatorStake, minDelegatorStake)
	require.Equal(utx.MaxValidatorWeightFactor, maxValidatorWeightFactor)
	require.Equal(utx.UptimeRequirement, uptimeRequirement)

	// check fee calculation
	require.Equal(
		addAmounts(
			addOutputAmounts(utx.Outs),
			map[ids.ID]uint64{
				avaxAssetID:   testContextPreEtna.StaticFeeConfig.TransformSubnetTxFee,
				subnetAssetID: maxSupply - initialSupply,
			},
		),
		addInputAmounts(utx.Ins),
	)
}

func TestAddPermissionlessValidatorTx(t *testing.T) {
	var utxosOffset uint64 = 2024
	makeUTXO := func(amount uint64) *avax.UTXO {
		utxosOffset++
		return &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(utxosOffset),
				OutputIndex: uint32(utxosOffset),
			},
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: utxoOwner,
			},
		}
	}

	var (
		utxos = []*avax.UTXO{
			makeUTXO(testContextPreEtna.StaticFeeConfig.AddPrimaryNetworkValidatorFee), // UTXO to pay the fee
			makeUTXO(1 * units.NanoAvax), // small UTXO
			makeUTXO(9 * units.Avax),     // large UTXO
		}

		// data to build the transaction
		validationRewardsOwner        = rewardsOwner
		delegationRewardsOwner        = rewardsOwner
		delegationShares       uint32 = reward.PercentDenominator
	)

	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	pop := signer.NewProofOfPossession(sk)

	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, nil)
				builder = builder.New(set.Of(utxoAddr, rewardAddr), e.context, backend)
			)

			// build the transaction
			utx, err := builder.NewAddPermissionlessValidatorTx(
				primaryNetworkPermissionlessStaker,
				pop,
				avaxAssetID,
				validationRewardsOwner,
				delegationRewardsOwner,
				delegationShares,
			)
			require.NoError(err)
			require.Equal(utx.Validator, primaryNetworkPermissionlessStaker.Validator)
			require.Equal(utx.Subnet, primaryNetworkPermissionlessStaker.Subnet)
			require.Equal(utx.Signer, pop)
			// Outputs should be merged if possible. For example, if there are two
			// unlocked inputs consumed for staking, this should only produce one staked
			// output.
			require.Len(utx.StakeOuts, 1)
			// check stake amount
			require.Equal(
				map[ids.ID]uint64{
					avaxAssetID: primaryNetworkPermissionlessStaker.Wght,
				},
				addOutputAmounts(utx.StakeOuts),
			)
			require.Equal(utx.ValidatorRewardsOwner, validationRewardsOwner)
			require.Equal(utx.DelegatorRewardsOwner, delegationRewardsOwner)
			require.Equal(utx.DelegationShares, delegationShares)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs, utx.StakeOuts),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

func TestAddPermissionlessDelegatorTx(t *testing.T) {
	for _, e := range testEnvironment {
		t.Run(e.name, func(t *testing.T) {
			var (
				require    = require.New(t)
				chainUTXOs = utxotest.NewDeterministicChainUTXOs(t, map[ids.ID][]*avax.UTXO{
					constants.PlatformChainID: utxos,
				})
				backend = NewBackend(e.context, chainUTXOs, nil)
				builder = builder.New(set.Of(utxoAddr, rewardAddr), e.context, backend)
			)

			// build the transaction
			utx, err := builder.NewAddPermissionlessDelegatorTx(
				primaryNetworkPermissionlessStaker,
				avaxAssetID,
				rewardsOwner,
			)
			require.NoError(err)
			require.Equal(utx.Validator, primaryNetworkPermissionlessStaker.Validator)
			require.Equal(utx.Subnet, primaryNetworkPermissionlessStaker.Subnet)
			// check stake amount
			require.Equal(
				map[ids.ID]uint64{
					avaxAssetID: primaryNetworkPermissionlessStaker.Wght,
				},
				addOutputAmounts(utx.StakeOuts),
			)
			require.Equal(utx.DelegationRewardsOwner, rewardsOwner)

			// check fee calculation
			expectedFee, err := e.feeCalculator.CalculateFee(utx)
			require.NoError(err)
			require.Equal(
				addAmounts(
					addOutputAmounts(utx.Outs, utx.StakeOuts),
					map[ids.ID]uint64{
						avaxAssetID: expectedFee,
					},
				),
				addInputAmounts(utx.Ins),
			)
		})
	}
}

func makeTestUTXOs(utxosKey *secp256k1.PrivateKey) []*avax.UTXO {
	// Note: we avoid ids.GenerateTestNodeID here to make sure that UTXO IDs
	// won't change run by run. This simplifies checking what utxos are included
	// in the built txs.
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
