// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpbench

import (
	"context"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

// To profile a specific benchmark, say BenchmarkGetCanonicalValidatorSetByDepth, run:
// go test -run=^$ -bench ^BenchmarkGetCanonicalValidatorSetByDepth$ github.com/ava-labs/avalanchego/vms/platformvm/warp/benchmarks -cpuprofile=cpuprofile.out
//
// To analyze results run:
// go tool pprof -web cpuprofile.out

func BenchmarkGetCanonicalValidatorSetBySize(b *testing.B) {
	b.StopTimer()
	require := require.New(b)

	// Prepare the bench environment (don't time it)
	subnetID := ids.GenerateTestID()
	env := newEnvironment(b, subnetID)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	pChainHeight := uint64(1)
	numNodes := 10_000

	// Store [numNodes] validators in state. They are all at the same height [pChainHeight]
	diff, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)
	for i := 0; i < numNodes; i++ {
		nodeID := ids.GenerateTestNodeID()

		// create primary network validator tx
		addPrimaryValidatorTx, err := env.txBuilder.NewAddValidatorTx(
			env.config.MinValidatorStake,
			uint64(env.state.GetTimestamp().Unix()),
			uint64(mockable.MaxTime.Unix()),
			nodeID,
			ids.ShortEmpty, // reward address
			reward.PercentDenominator,
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty,
		)
		require.NoError(err)

		// store corresponding primaryStaker in the diff
		primaryStaker, err := state.NewCurrentStaker(
			addPrimaryValidatorTx.ID(),
			addPrimaryValidatorTx.Unsigned.(*txs.AddValidatorTx),
			10000, // potential reward
		)
		require.NoError(err)
		diff.PutCurrentValidator(primaryStaker)
		diff.AddTx(addPrimaryValidatorTx, status.Committed)

		// create subnet validator tx
		blsPrivateKey, err := bls.NewSecretKey()
		require.NoError(err)

		uPermissionlessValidatorTx := &txs.AddPermissionlessValidatorTx{
			BaseTx: txs.BaseTx{
				BaseTx: avax.BaseTx{
					NetworkID:    1,
					BlockchainID: ids.GenerateTestID(),
					Outs: []*avax.TransferableOutput{{
						Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
						Out: &secp256k1fx.TransferOutput{
							Amt: uint64(1234),
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
							},
						},
					}},
					Ins: []*avax.TransferableInput{{
						UTXOID: avax.UTXOID{
							TxID:        ids.ID{'t', 'x', 'I', 'D'},
							OutputIndex: 2,
						},
						Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
						In: &secp256k1fx.TransferInput{
							Amt:   uint64(5678),
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Memo: []byte{1, 2, 3, 4, 5, 6, 7, 8},
				},
			},
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(env.state.GetTimestamp().Unix()),
				End:    uint64(mockable.MaxTime.Unix()),
				Wght:   20,
			},
			Subnet: subnetID,
			Signer: signer.NewProofOfPossession(blsPrivateKey),
			StakeOuts: []*avax.TransferableOutput{{
				Asset: avax.Asset{
					ID: ids.GenerateTestID(), // customAssetID
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			}},
			ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				Threshold: 1,
			},
			DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				Threshold: 1,
			},
			DelegationShares: reward.PercentDenominator,
		}
		permissionlessValidatorTx, err := txs.NewSigned(uPermissionlessValidatorTx, txs.Codec, nil)
		require.NoError(err)

		// store corresponding staker in the diff
		subnetStaker, err := state.NewCurrentStaker(
			permissionlessValidatorTx.ID(),
			permissionlessValidatorTx.Unsigned.(*txs.AddPermissionlessValidatorTx),
			10000, // dummy potential reward
		)
		require.NoError(err)
		diff.PutCurrentValidator(subnetStaker)
		diff.AddTx(permissionlessValidatorTx, status.Committed)
	}

	// Add a dummyBlock to update relevant quantities
	dummyValidatorsBlock := &blocks.BanffStandardBlock{
		ApricotStandardBlock: blocks.ApricotStandardBlock{
			CommonBlock: blocks.CommonBlock{
				PrntID:  env.state.GetLastAccepted(),
				Hght:    pChainHeight,
				BlockID: ids.GenerateTestID(),
			},
		},
	}

	// Push block and tx changes to env.state.
	require.NoError(diff.Apply(env.state))
	env.state.SetHeight(pChainHeight)
	env.state.AddStatelessBlock(dummyValidatorsBlock, choices.Accepted)
	env.state.SetLastAccepted(dummyValidatorsBlock.BlockID)
	require.NoError(env.state.Commit())

	b.StartTimer() // start testing
	for _, size := range []int{
		0,
		1,
		10,
		100,
		1000,
		numNodes,
	} {
		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _, err := warp.GetCanonicalValidatorSet(
					context.Background(),
					env.validatorsManager,
					pChainHeight,
					subnetID,
				)
				require.NoError(err)
			}
		})
	}
	b.StopTimer() // done testing
}

func BenchmarkGetCanonicalValidatorSetByDepth(b *testing.B) {
	b.StopTimer()
	require := require.New(b)

	// Prepare the bench environment (don't time it)
	subnetID := ids.GenerateTestID()
	env := newEnvironment(b, subnetID)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	validatorsCount := 100
	heightsRange := uint64(10)
	validatorsPerHeight := validatorsCount / int(heightsRange)

	// Store all primary network validators at height 1
	primaryValidatorIDs := make([]ids.NodeID, 0, validatorsCount)
	primaryDiff, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)
	for i := 0; i < validatorsCount; i++ {
		nodeID := ids.GenerateTestNodeID()
		primaryValidatorIDs = append(primaryValidatorIDs, nodeID)

		// create primary network validator tx
		addPrimaryValidatorTx, err := env.txBuilder.NewAddValidatorTx(
			env.config.MinValidatorStake,
			uint64(env.state.GetTimestamp().Unix()),
			uint64(mockable.MaxTime.Unix()),
			nodeID,
			ids.ShortEmpty, // reward address
			reward.PercentDenominator,
			[]*secp256k1.PrivateKey{preFundedKeys[0]},
			ids.ShortEmpty,
		)
		require.NoError(err)

		// store corresponding primaryStaker in the diff
		primaryStaker, err := state.NewCurrentStaker(
			addPrimaryValidatorTx.ID(),
			addPrimaryValidatorTx.Unsigned.(*txs.AddValidatorTx),
			10000, // potential reward
		)
		require.NoError(err)
		primaryDiff.PutCurrentValidator(primaryStaker)
		primaryDiff.AddTx(addPrimaryValidatorTx, status.Committed)
	}
	// Add a dummyBlock to update relevant quantities
	dummyPrimariesBlock := &blocks.BanffStandardBlock{
		ApricotStandardBlock: blocks.ApricotStandardBlock{
			CommonBlock: blocks.CommonBlock{
				PrntID:  env.state.GetLastAccepted(),
				Hght:    1,
				BlockID: ids.GenerateTestID(),
			},
		},
	}

	// Push block and tx changes to env.state.
	require.NoError(primaryDiff.Apply(env.state))
	env.state.SetHeight(1)
	env.state.AddStatelessBlock(dummyPrimariesBlock, choices.Accepted)
	env.state.SetLastAccepted(dummyPrimariesBlock.BlockID)
	require.NoError(env.state.Commit())

	// store subnet validators at different heights
	preSubnetValidatorsHeight := uint64(1)
	for height := preSubnetValidatorsHeight; height < preSubnetValidatorsHeight+heightsRange; height++ {
		subnetsValDiff, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
		require.NoError(err)

		for i := 0; i < validatorsPerHeight; i++ {
			nodeIdx := int(height-preSubnetValidatorsHeight)*validatorsPerHeight + i
			nodeID := primaryValidatorIDs[nodeIdx]

			// create subnet validator tx
			blsPrivateKey, err := bls.NewSecretKey()
			require.NoError(err)

			uPermissionlessValidatorTx := &txs.AddPermissionlessValidatorTx{
				BaseTx: txs.BaseTx{
					BaseTx: avax.BaseTx{
						NetworkID:    1,
						BlockchainID: ids.GenerateTestID(),
						Outs: []*avax.TransferableOutput{{
							Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
							Out: &secp256k1fx.TransferOutput{
								Amt: uint64(1234),
								OutputOwners: secp256k1fx.OutputOwners{
									Threshold: 1,
									Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
								},
							},
						}},
						Ins: []*avax.TransferableInput{{
							UTXOID: avax.UTXOID{
								TxID:        ids.ID{'t', 'x', 'I', 'D'},
								OutputIndex: 2,
							},
							Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 't'}},
							In: &secp256k1fx.TransferInput{
								Amt:   uint64(5678),
								Input: secp256k1fx.Input{SigIndices: []uint32{0}},
							},
						}},
						Memo: []byte{1, 2, 3, 4, 5, 6, 7, 8},
					},
				},
				Validator: txs.Validator{
					NodeID: nodeID,
					Start:  uint64(env.state.GetTimestamp().Unix()),
					End:    uint64(mockable.MaxTime.Unix()),
					Wght:   20,
				},
				Subnet: subnetID,
				Signer: signer.NewProofOfPossession(blsPrivateKey),
				StakeOuts: []*avax.TransferableOutput{{
					Asset: avax.Asset{
						ID: ids.GenerateTestID(), // customAssetID
					},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
						},
					},
				}},
				ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					Threshold: 1,
				},
				DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					Threshold: 1,
				},
				DelegationShares: reward.PercentDenominator,
			}
			permissionlessValidatorTx, err := txs.NewSigned(uPermissionlessValidatorTx, txs.Codec, nil)
			require.NoError(err)

			// store corresponding staker in the diff
			subnetStaker, err := state.NewCurrentStaker(
				permissionlessValidatorTx.ID(),
				permissionlessValidatorTx.Unsigned.(*txs.AddPermissionlessValidatorTx),
				10000, // dummy potential reward
			)
			require.NoError(err)
			subnetsValDiff.PutCurrentValidator(subnetStaker)
			subnetsValDiff.AddTx(permissionlessValidatorTx, status.Committed)
		}

		// Add a dummyBlock to update relevant quantities
		dummySecondaryBlock := &blocks.BanffStandardBlock{
			ApricotStandardBlock: blocks.ApricotStandardBlock{
				CommonBlock: blocks.CommonBlock{
					PrntID:  env.state.GetLastAccepted(),
					Hght:    height,
					BlockID: ids.GenerateTestID(),
				},
			},
		}

		// Push block and tx changes to env.state.
		require.NoError(subnetsValDiff.Apply(env.state))
		env.state.SetHeight(height)
		env.state.AddStatelessBlock(dummySecondaryBlock, choices.Accepted)
		env.state.SetLastAccepted(dummySecondaryBlock.BlockID)
		require.NoError(env.state.Commit())
	}

	b.StartTimer() // start testing
	for depth := uint64(0); depth < heightsRange; depth++ {
		b.Run(fmt.Sprintf("%d", depth), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _, err := warp.GetCanonicalValidatorSet(
					context.Background(),
					env.validatorsManager,
					heightsRange-depth,
					subnetID,
				)
				require.NoError(err)
			}
		})
	}
	b.StopTimer() // done testing
}
