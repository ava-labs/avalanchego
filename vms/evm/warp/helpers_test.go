// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var (
	networkID           uint32 = 54321
	sourceChainID              = ids.GenerateTestID()
	testSourceAddress          = utils.RandomBytes(20)
	testPayload                = []byte("test")
	testUnsignedMessage *warp.UnsignedMessage
)

func init() {
	testAddressedCallPayload, err := payload.NewAddressedCall(testSourceAddress, testPayload)
	if err != nil {
		panic(err)
	}
	testUnsignedMessage, err = warp.NewUnsignedMessage(networkID, sourceChainID, testAddressedCallPayload.Bytes())
	if err != nil {
		panic(err)
	}
}

type testBlockStore set.Set[ids.ID]

func (t testBlockStore) HasBlock(_ context.Context, blockID ids.ID) error {
	s := set.Set[ids.ID](t)
	if !s.Contains(blockID) {
		return database.ErrNotFound
	}
	return nil
}

// metricExpectations specifies expected metric values for test assertions
type metricExpectations struct {
	messageParseFail        int64
	addressedCallVerifyFail int64
	blockVerifyFail         int64
	uptimeVerifyFail        int64
}

// requireMetrics verifies all metrics in the Verifier match expectations
func requireMetrics(t *testing.T, v *Verifier, expected metricExpectations) {
	t.Helper()
	require.Equal(t, float64(expected.messageParseFail), testutil.ToFloat64(v.messageParseFail))
	require.Equal(t, float64(expected.addressedCallVerifyFail), testutil.ToFloat64(v.addressedCallVerifyFail))
	require.Equal(t, float64(expected.blockVerifyFail), testutil.ToFloat64(v.blockVerifyFail))
	require.Equal(t, float64(expected.uptimeVerifyFail), testutil.ToFloat64(v.uptimeVerifyFail))
}

// handlerTestSetup contains common test fixtures for handler tests
type handlerTestSetup struct {
	client       *p2p.Client
	verifier     *Verifier
	db           *DB
	pk           *bls.PublicKey
	sigCache     *lru.Cache[ids.ID, []byte]
	serverNodeID ids.NodeID
}

// setupHandler creates a handler with p2p client for testing
func setupHandler(
	t *testing.T,
	ctx context.Context,
	database database.Database,
	blockStore BlockStore,
	uptimeTracker *uptimetracker.UptimeTracker,
	networkID uint32,
	chainID ids.ID,
) *handlerTestSetup {
	sk, err := localsigner.New()
	require.NoError(t, err)
	pk := sk.PublicKey()
	signer := warp.NewSigner(sk, networkID, chainID)

	sigCache := lru.NewCache[ids.ID, []byte](100)
	db := NewDB(database)
	v := NewVerifier(db, blockStore, uptimeTracker, prometheus.NewRegistry())
	handler := NewHandler(sigCache, v, signer)

	clientNodeID := ids.GenerateTestNodeID()
	serverNodeID := ids.GenerateTestNodeID()
	client := p2ptest.NewClient(t, ctx, clientNodeID, p2p.NoOpHandler{}, serverNodeID, handler)

	return &handlerTestSetup{
		client:       client,
		verifier:     v,
		db:           db,
		pk:           pk,
		sigCache:     sigCache,
		serverNodeID: serverNodeID,
	}
}

// sendSignatureRequest sends a signature request and returns the signature bytes and any error
func sendSignatureRequest(
	t *testing.T,
	ctx context.Context,
	setup *handlerTestSetup,
	message *warp.UnsignedMessage,
) ([]byte, *common.AppError) {
	t.Helper()

	request := &sdk.SignatureRequest{
		Message: message.Bytes(),
	}
	requestBytes, err := proto.Marshal(request)
	require.NoError(t, err)

	var signature []byte
	var appErr *common.AppError
	responseChan := make(chan struct{})
	onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		defer close(responseChan)

		if err != nil {
			appErr, _ = err.(*common.AppError)
			return
		}

		var response sdk.SignatureResponse
		require.NoError(t, proto.Unmarshal(responseBytes, &response))
		signature = response.Signature

		sig, err := bls.SignatureFromBytes(response.Signature)
		require.NoError(t, err)
		require.True(t, bls.Verify(setup.pk, sig, request.Message))
	}

	require.NoError(t, setup.client.AppRequest(ctx, set.Of(setup.serverNodeID), requestBytes, onResponse))
	<-responseChan

	return signature, appErr
}
