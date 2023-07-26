// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/stretchr/testify/require"
)

type mockFetcher struct {
	fetch func(ctx context.Context, nodeID ids.NodeID, unsignedWarpMessage *avalancheWarp.UnsignedMessage) (*bls.Signature, error)
}

func (m *mockFetcher) FetchWarpSignature(ctx context.Context, nodeID ids.NodeID, unsignedWarpMessage *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
	return m.fetch(ctx, nodeID, unsignedWarpMessage)
}

var (
	nodeIDs       []ids.NodeID
	blsSecretKeys []*bls.SecretKey
	blsPublicKeys []*bls.PublicKey
	networkID     uint32 = 54321
	sourceChainID        = ids.GenerateTestID()
	unsignedMsg   *avalancheWarp.UnsignedMessage
	blsSignatures []*bls.Signature
)

func init() {
	var err error
	unsignedMsg, err = avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, []byte{1, 2, 3})
	if err != nil {
		panic(err)
	}
	for i := 0; i < 5; i++ {
		nodeIDs = append(nodeIDs, ids.GenerateTestNodeID())

		blsSecretKey, err := bls.NewSecretKey()
		if err != nil {
			panic(err)
		}
		blsPublicKey := bls.PublicFromSecretKey(blsSecretKey)
		blsSignature := bls.Sign(blsSecretKey, unsignedMsg.Bytes())
		blsSecretKeys = append(blsSecretKeys, blsSecretKey)
		blsPublicKeys = append(blsPublicKeys, blsPublicKey)
		blsSignatures = append(blsSignatures, blsSignature)
	}
}

type signatureJobTest struct {
	ctx               context.Context
	job               *signatureJob
	expectedSignature *bls.Signature
	expectedErr       error
}

func executeSignatureJobTest(t testing.TB, test signatureJobTest) {
	t.Helper()

	blsSignature, err := test.job.Execute(test.ctx)
	if test.expectedErr != nil {
		require.ErrorIs(t, err, test.expectedErr)
		return
	}
	require.NoError(t, err)
	require.Equal(t, bls.SignatureToBytes(blsSignature), bls.SignatureToBytes(test.expectedSignature))
}

func TestSignatureRequestSuccess(t *testing.T) {
	job := newSignatureJob(
		&mockFetcher{
			fetch: func(context.Context, ids.NodeID, *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				return blsSignatures[0], nil
			},
		},
		&avalancheWarp.Validator{
			NodeIDs:   nodeIDs[:1],
			PublicKey: blsPublicKeys[0],
			Weight:    10,
		},
		unsignedMsg,
	)

	executeSignatureJobTest(t, signatureJobTest{
		ctx:               context.Background(),
		job:               job,
		expectedSignature: blsSignatures[0],
	})
}

func TestSignatureRequestFails(t *testing.T) {
	err := errors.New("expected error")
	job := newSignatureJob(
		&mockFetcher{
			fetch: func(context.Context, ids.NodeID, *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				return nil, err
			},
		},
		&avalancheWarp.Validator{
			NodeIDs:   nodeIDs[:1],
			PublicKey: blsPublicKeys[0],
			Weight:    10,
		},
		unsignedMsg,
	)

	executeSignatureJobTest(t, signatureJobTest{
		ctx:         context.Background(),
		job:         job,
		expectedErr: err,
	})
}

func TestSignatureRequestInvalidSignature(t *testing.T) {
	job := newSignatureJob(
		&mockFetcher{
			fetch: func(context.Context, ids.NodeID, *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
				return blsSignatures[1], nil
			},
		},
		&avalancheWarp.Validator{
			NodeIDs:   nodeIDs[:1],
			PublicKey: blsPublicKeys[0],
			Weight:    10,
		},
		unsignedMsg,
	)

	executeSignatureJobTest(t, signatureJobTest{
		ctx:         context.Background(),
		job:         job,
		expectedErr: errInvalidSignature,
	})
}
