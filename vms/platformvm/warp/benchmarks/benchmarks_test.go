// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpbench

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
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

// To profile a specific benchmark, say BenchmarkGetCanonicalValidatorSetBySize, run:
// go test -run=^$ -bench ^BenchmarkGetCanonicalValidatorSetBySize$ github.com/ava-labs/avalanchego/vms/platformvm/warp/benchmarks -cpuprofile=cpuprofile.out
//
// To analyze results run:
// go tool pprof -web cpuprofile.out

func BenchmarkGetCanonicalValidatorSetBySize(b *testing.B) {
	require := require.New(b)
	var (
		subnetID            = ids.GenerateTestID()
		pChainHeight        = uint64(1)
		numNodes            = 10_000
		getValidatorOutputs = make([]*validators.GetValidatorOutput, 0, numNodes)
	)

	for i := 0; i < numNodes; i++ {
		nodeID := ids.GenerateTestNodeID()
		blsPrivateKey, err := bls.NewSecretKey()
		require.NoError(err)
		blsPublicKey := bls.PublicFromSecretKey(blsPrivateKey)
		getValidatorOutputs = append(getValidatorOutputs, &validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: blsPublicKey,
			Weight:    20,
		})
	}

	for _, valCounts := range []int{0, 1, 10, 100, 1_000, 10_000} {
		getValidatorsOutput := make(map[ids.NodeID]*validators.GetValidatorOutput)
		for i := 0; i < valCounts; i++ {
			validator := getValidatorOutputs[i]
			getValidatorsOutput[validator.NodeID] = validator
		}
		validatorState := &validators.TestState{
			GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				return getValidatorsOutput, nil
			},
		}

		b.Run(fmt.Sprintf("%d", valCounts), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				vals, _, err := warp.GetCanonicalValidatorSet(
					context.Background(),
					validatorState,
					pChainHeight,
					subnetID,
				)
				require.NoError(err)
				require.Len(vals, valCounts)
			}
		})
	}
}

func BenchmarkGetCanonicalValidatorSetByDepth(b *testing.B) {
	require := require.New(b)
	var (
		validatorsCount     = 10_000
		heightsRange        = uint64(20)
		validatorsPerHeight = validatorsCount / int(heightsRange)

		subnetID            = ids.GenerateTestID()
		getValidatorOutputs = make(map[uint64](map[ids.NodeID]*validators.GetValidatorOutput))
	)

	for depth := uint64(0); depth < heightsRange; depth++ {
		getValidatorOutputs[heightsRange-depth] = make(map[ids.NodeID]*validators.GetValidatorOutput)
		for i := 0; i < validatorsPerHeight; i++ {
			nodeID := ids.GenerateTestNodeID()
			blsPrivateKey, err := bls.NewSecretKey()
			require.NoError(err)
			blsPublicKey := bls.PublicFromSecretKey(blsPrivateKey)
			getValidatorOutputs[heightsRange-depth][nodeID] = &validators.GetValidatorOutput{
				NodeID:    nodeID,
				PublicKey: blsPublicKey,
				Weight:    20,
			}
		}
	}
	validatorState := &validators.TestState{
		GetValidatorSetF: func(_ context.Context, depth uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			res, found := getValidatorOutputs[depth]
			if !found {
				return nil, database.ErrNotFound
			}
			return res, nil
		},
	}

	b.StartTimer() // start testing
	for depth := uint64(0); depth < heightsRange; depth++ {
		b.Run(fmt.Sprintf("%d", depth), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				vals, _, err := warp.GetCanonicalValidatorSet(
					context.Background(),
					validatorState,
					heightsRange-depth,
					subnetID,
				)
				require.NoError(err)
				require.Len(vals, validatorsPerHeight)
			}
		})
	}
	b.StopTimer() // done testing
}

func BenchmarkGetValidatorSetBySize_Validators_100(b *testing.B) {
	benchGetValidatorSetBySize(b, 100)
}

func BenchmarkGetValidatorSetBySize_Validators_1000(b *testing.B) {
	benchGetValidatorSetBySize(b, 1000)
}

func BenchmarkGetValidatorSetBySize_Validators_10000(b *testing.B) {
	benchGetValidatorSetBySize(b, 10_000)
}

func benchGetValidatorSetBySize(b *testing.B, validatorsCount int) {
	b.StopTimer()
	require := require.New(b)

	// Prepare the bench environment (don't time it)
	subnetID := ids.GenerateTestID()
	env := newEnvironment(b, subnetID)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	pChainHeight := uint64(1)

	// Store [validatorsCount] validators in state. They are all at the same height [pChainHeight]
	diff, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)
	for i := 0; i < validatorsCount; i++ {
		nodeID := ids.GenerateTestNodeID()

		// create primary network validator tx
		addPrimaryValidatorTx, err := createPrimaryNetworkValidatorTx(nodeID, env.state.GetTimestamp())
		require.NoError(err)

		// store corresponding primaryStaker in the diff
		primaryStaker, err := state.NewCurrentStaker(
			addPrimaryValidatorTx.ID(),
			addPrimaryValidatorTx.Unsigned.(*txs.AddPermissionlessValidatorTx),
			10000, // potential reward
		)
		require.NoError(err)
		diff.PutCurrentValidator(primaryStaker)
		diff.AddTx(addPrimaryValidatorTx, status.Committed)

		// create subnet validator tx
		permissionlessValidatorTx, err := createSubnetValidatorTx(subnetID, nodeID, env.state.GetTimestamp())
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
	dummyValidatorsBlock, err := blocks.NewBanffStandardBlock(
		env.state.GetTimestamp(),
		env.state.GetLastAccepted(),
		pChainHeight,
		[]*txs.Tx{},
	)
	require.NoError(err)

	// Push block and tx changes to env.state.
	require.NoError(diff.Apply(env.state))
	env.state.SetHeight(pChainHeight)
	env.state.AddStatelessBlock(dummyValidatorsBlock, choices.Accepted)
	env.state.SetLastAccepted(dummyValidatorsBlock.BlockID)
	require.NoError(env.state.Commit())

	b.StartTimer() // start testing
	for i := 0; i < b.N; i++ {
		vals, err := env.validatorsManager.GetValidatorSet(
			context.Background(),
			pChainHeight,
			subnetID,
		)
		require.NoError(err)
		require.Len(vals, validatorsCount)
	}
	b.StopTimer() // done testing
}

func BenchmarkGetValidatorSetByHeight_Validators_1000(b *testing.B) {
	benchGetValidatorSetByHeight(b, 1000, 10)
}

func benchGetValidatorSetByHeight(b *testing.B, validatorsCount int, heightsRange uint64) {
	b.StopTimer()
	require := require.New(b)

	// Prepare the bench environment (don't time it)
	subnetID := ids.GenerateTestID()
	env := newEnvironment(b, subnetID)
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	validatorsPerHeight := validatorsCount / int(heightsRange)

	// Store all primary network validators at height 1
	primaryValidatorIDs := make([]ids.NodeID, 0, validatorsCount)
	primaryDiff, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
	require.NoError(err)
	for i := 0; i < validatorsCount; i++ {
		nodeID := ids.GenerateTestNodeID()
		primaryValidatorIDs = append(primaryValidatorIDs, nodeID)

		// create primary network validator tx
		addPrimaryValidatorTx, err := createPrimaryNetworkValidatorTx(nodeID, env.state.GetTimestamp())
		require.NoError(err)

		// store corresponding primaryStaker in the diff
		primaryStaker, err := state.NewCurrentStaker(
			addPrimaryValidatorTx.ID(),
			addPrimaryValidatorTx.Unsigned.(*txs.AddPermissionlessValidatorTx),
			10000, // potential reward
		)
		require.NoError(err)
		primaryDiff.PutCurrentValidator(primaryStaker)
		primaryDiff.AddTx(addPrimaryValidatorTx, status.Committed)
	}
	// Add a dummyBlock to update relevant quantities
	dummyPrimariesBlock, err := blocks.NewBanffStandardBlock(
		env.state.GetTimestamp(),
		env.state.GetLastAccepted(),
		1,
		[]*txs.Tx{},
	)
	require.NoError(err)

	// Push block and tx changes to env.state.
	require.NoError(primaryDiff.Apply(env.state))
	env.state.SetHeight(1)
	env.state.AddStatelessBlock(dummyPrimariesBlock, choices.Accepted)
	env.state.SetLastAccepted(dummyPrimariesBlock.BlockID)
	require.NoError(env.state.Commit())

	// store subnet validators at different heights
	startHeight := uint64(1) + 1
	for height := startHeight; height < startHeight+heightsRange; height++ {
		subnetsValDiff, err := state.NewDiff(env.state.GetLastAccepted(), env.blkManager)
		require.NoError(err)

		for i := 0; i < validatorsPerHeight; i++ {
			nodeIdx := int(height-startHeight)*validatorsPerHeight + i
			nodeID := primaryValidatorIDs[nodeIdx]

			// create subnet validator tx
			permissionlessValidatorTx, err := createSubnetValidatorTx(subnetID, nodeID, env.state.GetTimestamp())
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
		dummySecondaryBlock, err := blocks.NewBanffStandardBlock(
			env.state.GetTimestamp(),
			env.state.GetLastAccepted(),
			height,
			[]*txs.Tx{},
		)
		require.NoError(err)

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
				vals, err := env.validatorsManager.GetValidatorSet(
					context.Background(),
					heightsRange-depth,
					subnetID,
				)
				require.NoError(err)
				require.Len(vals, validatorsCount-int(depth+1)*validatorsPerHeight)
			}
		})
	}
	b.StopTimer() // done testing
}

// createPrimaryNetworkValidatorTx returns a tx used in benchmarks to add a primary network validator.
func createPrimaryNetworkValidatorTx(nodeID ids.NodeID, currentChainTime time.Time) (*txs.Tx, error) {
	blsPrivateKey, err := bls.NewSecretKey()
	if err != nil {
		return nil, err
	}

	uPermissionlessPrimaryValidatorTx := &txs.AddPermissionlessValidatorTx{
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
			Start:  uint64(currentChainTime.Unix()),
			End:    uint64(mockable.MaxTime.Unix()),
			Wght:   20,
		},
		Subnet: constants.PrimaryNetworkID,
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

	return txs.NewSigned(uPermissionlessPrimaryValidatorTx, txs.Codec, nil)
}

// createSubnetValidatorTx returns a tx used in benchmarks to add a subnet validator.
func createSubnetValidatorTx(subnetID ids.ID, nodeID ids.NodeID, currentChainTime time.Time) (*txs.Tx, error) {
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
			Start:  uint64(currentChainTime.Unix()),
			End:    uint64(mockable.MaxTime.Unix()),
			Wght:   20,
		},
		Subnet: subnetID,

		// Note: the corresponding primary network validator has the BLS key
		// that is returned by GetCanonicalValidatorSet. Returning an empty signer here
		Signer: &signer.Empty{},

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

	return txs.NewSigned(uPermissionlessValidatorTx, txs.Codec, nil)
}
