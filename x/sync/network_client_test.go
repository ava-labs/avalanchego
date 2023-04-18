// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

var testPeerVersion = version.CurrentApp

func TestAppRequestOnShutdown(t *testing.T) {
	var (
		net    NetworkClient
		wg     sync.WaitGroup
		called bool
	)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sender := common.NewMockSender(ctrl)
	sender.EXPECT().SendAppRequest(
		gomock.Any(), // ctx
		gomock.Any(), // nodeIDs
		gomock.Any(), // requestID
		gomock.Any(), // requestBytes
	).DoAndReturn(
		func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
			wg.Add(1)
			go func() {
				called = true
				// shutdown the network here to ensure any outstanding requests are handled as failed
				net.Shutdown()
				wg.Done()
			}() // this is on a goroutine to avoid a deadlock since calling Shutdown takes the lock.
			return nil
		},
	)

	net = NewNetworkClient(sender, ids.EmptyNodeID, 1, logging.NoLog{})
	nodeID := ids.GenerateTestNodeID()
	require.NoError(t, net.Connected(context.Background(), nodeID, testPeerVersion))

	wg.Add(1)
	go func() {
		defer wg.Done()
		requestBytes := []byte("message")
		responseBytes, _, err := net.RequestAny(context.Background(), testPeerVersion, requestBytes)
		require.Error(t, err, ErrRequestFailed)
		require.Nil(t, responseBytes)
	}()
	wg.Wait()
	require.True(t, called)
}
