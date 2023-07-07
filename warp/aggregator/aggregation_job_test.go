// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/stretchr/testify/require"
)

var (
	subnetID          = ids.GenerateTestID()
	pChainHeight      = uint64(10)
	getSubnetIDF      = func(ctx context.Context, chainID ids.ID) (ids.ID, error) { return subnetID, nil }
	getCurrentHeightF = func(ctx context.Context) (uint64, error) { return pChainHeight, nil }
)

type signatureAggregationTest struct {
	ctx         context.Context
	job         *signatureAggregationJob
	expectedRes *AggregateSignatureResult
	expectedErr error
}

func executeSignatureAggregationTest(t testing.TB, test signatureAggregationTest) {
	t.Helper()

	res, err := test.job.Execute(test.ctx)
	if test.expectedErr != nil {
		require.ErrorIs(t, err, test.expectedErr)
		return
	}

	require.Equal(t, res.SignatureWeight, test.expectedRes.SignatureWeight)
	require.Equal(t, res.TotalWeight, test.expectedRes.TotalWeight)
	require.NoError(t, res.Message.Signature.Verify(
		context.Background(),
		&res.Message.UnsignedMessage,
		test.job.state,
		pChainHeight,
		test.job.minValidQuorumNum,
		test.job.quorumDen,
	))
}

func TestSingleSignatureAggregator(t *testing.T) {
	ctx := context.Background()
	aggregationJob := newSignatureAggregationJob(
		&mockFetcher{
			fetch: func(context.Context, ids.NodeID, *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				return blsSignatures[0], nil
			},
		},
		pChainHeight,
		subnetID,
		100,
		100,
		100,
		&validators.TestState{
			GetSubnetIDF:      getSubnetIDF,
			GetCurrentHeightF: getCurrentHeightF,
			GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				return map[ids.NodeID]*validators.GetValidatorOutput{
					nodeIDs[0]: {
						NodeID:    nodeIDs[0],
						PublicKey: blsPublicKeys[0],
						Weight:    100,
					},
				}, nil
			},
		},
		unsignedMsg,
	)

	signature := &avalancheWarp.BitSetSignature{
		Signers: set.NewBits(0).Bytes(),
	}
	signedMessage, err := avalancheWarp.NewMessage(unsignedMsg, signature)
	require.NoError(t, err)
	copy(signature.Signature[:], bls.SignatureToBytes(blsSignatures[0]))
	expectedRes := &AggregateSignatureResult{
		SignatureWeight: 100,
		TotalWeight:     100,
		Message:         signedMessage,
	}
	executeSignatureAggregationTest(t, signatureAggregationTest{
		ctx:         ctx,
		job:         aggregationJob,
		expectedRes: expectedRes,
	})
}

func TestAggregateAllSignatures(t *testing.T) {
	ctx := context.Background()
	aggregationJob := newSignatureAggregationJob(
		&mockFetcher{
			fetch: func(_ context.Context, nodeID ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				for i, matchingNodeID := range nodeIDs {
					if matchingNodeID == nodeID {
						return blsSignatures[i], nil
					}
				}
				panic("request to unexpected nodeID")
			},
		},
		pChainHeight,
		subnetID,
		100,
		100,
		100,
		&validators.TestState{
			GetSubnetIDF:      getSubnetIDF,
			GetCurrentHeightF: getCurrentHeightF,
			GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				res := make(map[ids.NodeID]*validators.GetValidatorOutput)
				for i := 0; i < 5; i++ {
					res[nodeIDs[i]] = &validators.GetValidatorOutput{
						NodeID:    nodeIDs[i],
						PublicKey: blsPublicKeys[i],
						Weight:    100,
					}
				}
				return res, nil
			},
		},
		unsignedMsg,
	)

	signature := &avalancheWarp.BitSetSignature{
		Signers: set.NewBits(0, 1, 2, 3, 4).Bytes(),
	}
	signedMessage, err := avalancheWarp.NewMessage(unsignedMsg, signature)
	require.NoError(t, err)
	aggregateSignature, err := bls.AggregateSignatures(blsSignatures)
	require.NoError(t, err)
	copy(signature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	expectedRes := &AggregateSignatureResult{
		SignatureWeight: 500,
		TotalWeight:     500,
		Message:         signedMessage,
	}
	executeSignatureAggregationTest(t, signatureAggregationTest{
		ctx:         ctx,
		job:         aggregationJob,
		expectedRes: expectedRes,
	})
}

func TestAggregateThresholdSignatures(t *testing.T) {
	ctx := context.Background()
	aggregationJob := newSignatureAggregationJob(
		&mockFetcher{
			fetch: func(_ context.Context, nodeID ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				for i, matchingNodeID := range nodeIDs[:3] {
					if matchingNodeID == nodeID {
						return blsSignatures[i], nil
					}
				}
				return nil, errors.New("what do we say to the god of death")
			},
		},
		pChainHeight,
		subnetID,
		60,
		60,
		100,
		&validators.TestState{
			GetSubnetIDF:      getSubnetIDF,
			GetCurrentHeightF: getCurrentHeightF,
			GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				res := make(map[ids.NodeID]*validators.GetValidatorOutput)
				for i := 0; i < 5; i++ {
					res[nodeIDs[i]] = &validators.GetValidatorOutput{
						NodeID:    nodeIDs[i],
						PublicKey: blsPublicKeys[i],
						Weight:    100,
					}
				}
				return res, nil
			},
		},
		unsignedMsg,
	)

	signature := &avalancheWarp.BitSetSignature{
		Signers: set.NewBits(0, 1, 2).Bytes(),
	}
	signedMessage, err := avalancheWarp.NewMessage(unsignedMsg, signature)
	require.NoError(t, err)
	aggregateSignature, err := bls.AggregateSignatures(blsSignatures)
	require.NoError(t, err)
	copy(signature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	expectedRes := &AggregateSignatureResult{
		SignatureWeight: 300,
		TotalWeight:     500,
		Message:         signedMessage,
	}
	executeSignatureAggregationTest(t, signatureAggregationTest{
		ctx:         ctx,
		job:         aggregationJob,
		expectedRes: expectedRes,
	})
}

func TestAggregateThresholdSignaturesInsufficientWeight(t *testing.T) {
	ctx := context.Background()
	aggregationJob := newSignatureAggregationJob(
		&mockFetcher{
			fetch: func(_ context.Context, nodeID ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				for i, matchingNodeID := range nodeIDs[:3] {
					if matchingNodeID == nodeID {
						return blsSignatures[i], nil
					}
				}
				return nil, errors.New("what do we say to the god of death")
			},
		},
		pChainHeight,
		subnetID,
		80,
		80,
		100,
		&validators.TestState{
			GetSubnetIDF:      getSubnetIDF,
			GetCurrentHeightF: getCurrentHeightF,
			GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				res := make(map[ids.NodeID]*validators.GetValidatorOutput)
				for i := 0; i < 5; i++ {
					res[nodeIDs[i]] = &validators.GetValidatorOutput{
						NodeID:    nodeIDs[i],
						PublicKey: blsPublicKeys[i],
						Weight:    100,
					}
				}
				return res, nil
			},
		},
		unsignedMsg,
	)

	executeSignatureAggregationTest(t, signatureAggregationTest{
		ctx:         ctx,
		job:         aggregationJob,
		expectedErr: avalancheWarp.ErrInsufficientWeight,
	})
}

func TestAggregateThresholdSignaturesBlockingRequests(t *testing.T) {
	ctx := context.Background()
	aggregationJob := newSignatureAggregationJob(
		&mockFetcher{
			fetch: func(ctx context.Context, nodeID ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				for i, matchingNodeID := range nodeIDs[:3] {
					if matchingNodeID == nodeID {
						return blsSignatures[i], nil
					}
				}

				// Block until the context is cancelled and return the error if not available
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
		pChainHeight,
		subnetID,
		60,
		60,
		100,
		&validators.TestState{
			GetSubnetIDF:      getSubnetIDF,
			GetCurrentHeightF: getCurrentHeightF,
			GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				res := make(map[ids.NodeID]*validators.GetValidatorOutput)
				for i := 0; i < 5; i++ {
					res[nodeIDs[i]] = &validators.GetValidatorOutput{
						NodeID:    nodeIDs[i],
						PublicKey: blsPublicKeys[i],
						Weight:    100,
					}
				}
				return res, nil
			},
		},
		unsignedMsg,
	)

	signature := &avalancheWarp.BitSetSignature{
		Signers: set.NewBits(0, 1, 2).Bytes(),
	}
	signedMessage, err := avalancheWarp.NewMessage(unsignedMsg, signature)
	require.NoError(t, err)
	aggregateSignature, err := bls.AggregateSignatures(blsSignatures)
	require.NoError(t, err)
	copy(signature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	expectedRes := &AggregateSignatureResult{
		SignatureWeight: 300,
		TotalWeight:     500,
		Message:         signedMessage,
	}
	executeSignatureAggregationTest(t, signatureAggregationTest{
		ctx:         ctx,
		job:         aggregationJob,
		expectedRes: expectedRes,
	})
}

func TestAggregateThresholdSignaturesParentCtxCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	aggregationJob := newSignatureAggregationJob(
		&mockFetcher{
			fetch: func(ctx context.Context, nodeID ids.NodeID, _ *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				// Block until the context is cancelled and return the error if not available
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
		pChainHeight,
		subnetID,
		60,
		60,
		100,
		&validators.TestState{
			GetSubnetIDF:      getSubnetIDF,
			GetCurrentHeightF: getCurrentHeightF,
			GetValidatorSetF: func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				res := make(map[ids.NodeID]*validators.GetValidatorOutput)
				for i := 0; i < 5; i++ {
					res[nodeIDs[i]] = &validators.GetValidatorOutput{
						NodeID:    nodeIDs[i],
						PublicKey: blsPublicKeys[i],
						Weight:    100,
					}
				}
				return res, nil
			},
		},
		unsignedMsg,
	)

	executeSignatureAggregationTest(t, signatureAggregationTest{
		ctx:         ctx,
		job:         aggregationJob,
		expectedErr: avalancheWarp.ErrInsufficientWeight,
	})
}
