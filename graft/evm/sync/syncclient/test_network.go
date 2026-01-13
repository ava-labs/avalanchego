// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncclient

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
)

var _ types.SyncedNetworkClient = (*TestNetwork)(nil)

// TestNetwork is a mock network client for testing state sync.
type TestNetwork struct {
	// NumCalls tracks the number of requests made
	NumCalls uint

	// Response holds the responses to return
	Response       [][]byte
	Callback       func() // Callback is called prior to processing each test call
	RequestErr     []error
	NodesRequested []ids.NodeID
}

func (t *TestNetwork) SendSyncedAppRequestAny(_ context.Context, _ *version.Application, _ []byte) ([]byte, ids.NodeID, error) {
	if len(t.Response) == 0 {
		return nil, ids.EmptyNodeID, errors.New("no tested response to return in TestNetwork")
	}

	response, err := t.processTest()
	return response, ids.EmptyNodeID, err
}

func (t *TestNetwork) SendSyncedAppRequest(_ context.Context, nodeID ids.NodeID, _ []byte) ([]byte, error) {
	if len(t.Response) == 0 {
		return nil, errors.New("no tested response to return in TestNetwork")
	}

	t.NodesRequested = append(t.NodesRequested, nodeID)

	return t.processTest()
}

func (t *TestNetwork) processTest() ([]byte, error) {
	t.NumCalls++

	if t.Callback != nil {
		t.Callback()
	}

	response := t.Response[0]
	if len(t.Response) > 1 {
		t.Response = t.Response[1:]
	} else {
		t.Response = nil
	}

	var err error
	if len(t.RequestErr) > 0 {
		err = t.RequestErr[0]
		t.RequestErr = t.RequestErr[1:]
	}

	return response, err
}

func (*TestNetwork) Gossip([]byte) error {
	panic("not implemented") // we don't care about this function for this test
}

// TestResponse sets up a single response to be returned `times` number of times.
func (t *TestNetwork) TestResponse(times uint8, callback func(), response []byte) {
	t.Response = make([][]byte, times)
	for i := uint8(0); i < times; i++ {
		t.Response[i] = response
	}
	t.Callback = callback
	t.NumCalls = 0
}

// TestResponses sets up multiple responses to be returned in sequence.
func (t *TestNetwork) TestResponses(callback func(), responses ...[]byte) {
	t.Response = responses
	t.Callback = callback
	t.NumCalls = 0
}

func (*TestNetwork) TrackBandwidth(ids.NodeID, float64) {}
