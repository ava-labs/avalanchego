// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

// NewClient generates a client-server pair and returns the client used to
// communicate with a server with the specified handler
func NewClient(
	t *testing.T,
	ctx context.Context,
	handler p2p.Handler,
	clientNodeID ids.NodeID,
	serverNodeID ids.NodeID,
) *p2p.Client {
	clientSender := &enginetest.Sender{}
	serverSender := &enginetest.Sender{}

	clientNetwork, err := p2p.NewNetwork(logging.NoLog{}, clientSender, prometheus.NewRegistry(), "")
	require.NoError(t, err)

	serverNetwork, err := p2p.NewNetwork(logging.NoLog{}, serverSender, prometheus.NewRegistry(), "")
	require.NoError(t, err)

	clientSender.SendAppGossipF = func(ctx context.Context, _ common.SendConfig, gossipBytes []byte) error {
		// Send the request asynchronously to avoid deadlock when the server
		// sends the response back to the client
		go func() {
			require.NoError(t, serverNetwork.AppGossip(ctx, clientNodeID, gossipBytes))
		}()

		return nil
	}

	clientSender.SendAppRequestF = func(ctx context.Context, _ set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
		// Send the request asynchronously to avoid deadlock when the server
		// sends the response back to the client
		go func() {
			require.NoError(t, serverNetwork.AppRequest(ctx, clientNodeID, requestID, time.Time{}, requestBytes))
		}()

		return nil
	}

	serverSender.SendAppResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
		// Send the request asynchronously to avoid deadlock when the server
		// sends the response back to the client
		go func() {
			require.NoError(t, clientNetwork.AppResponse(ctx, serverNodeID, requestID, responseBytes))
		}()

		return nil
	}

	serverSender.SendAppErrorF = func(ctx context.Context, _ ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
		// Send the request asynchronously to avoid deadlock when the server
		// sends the response back to the client
		go func() {
			require.NoError(t, clientNetwork.AppRequestFailed(ctx, serverNodeID, requestID, &common.AppError{
				Code:    errorCode,
				Message: errorMessage,
			}))
		}()

		return nil
	}

	require.NoError(t, clientNetwork.Connected(ctx, clientNodeID, nil))
	require.NoError(t, clientNetwork.Connected(ctx, serverNodeID, nil))
	require.NoError(t, serverNetwork.Connected(ctx, clientNodeID, nil))
	require.NoError(t, serverNetwork.Connected(ctx, serverNodeID, nil))

	require.NoError(t, serverNetwork.AddHandler(0, handler))
	return clientNetwork.NewClient(0)
}
