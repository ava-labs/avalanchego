// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"

	"github.com/ava-labs/coreth/network"
)

var _ network.SyncedNetworkClient = (*testNetwork)(nil)

type testNetwork struct {
	// captured request data
	numCalls         uint
	requestedVersion *version.Application
	request          []byte

	// response testing for RequestAny and Request calls
	response       [][]byte
	callback       func() // callback is called prior to processing each test call
	requestErr     []error
	nodesRequested []ids.NodeID
}

func (t *testNetwork) SendSyncedAppRequestAny(ctx context.Context, minVersion *version.Application, request []byte) ([]byte, ids.NodeID, error) {
	if len(t.response) == 0 {
		return nil, ids.EmptyNodeID, errors.New("no tested response to return in testNetwork")
	}

	t.requestedVersion = minVersion

	response, err := t.processTest(request)
	return response, ids.EmptyNodeID, err
}

func (t *testNetwork) SendSyncedAppRequest(ctx context.Context, nodeID ids.NodeID, request []byte) ([]byte, error) {
	if len(t.response) == 0 {
		return nil, errors.New("no tested response to return in testNetwork")
	}

	t.nodesRequested = append(t.nodesRequested, nodeID)

	return t.processTest(request)
}

func (t *testNetwork) processTest(request []byte) ([]byte, error) {
	t.request = request
	t.numCalls++

	if t.callback != nil {
		t.callback()
	}

	response := t.response[0]
	if len(t.response) > 1 {
		t.response = t.response[1:]
	} else {
		t.response = nil
	}

	var err error
	if len(t.requestErr) > 0 {
		err = t.requestErr[0]
		t.requestErr = t.requestErr[1:]
	}

	return response, err
}

func (t *testNetwork) Gossip([]byte) error {
	panic("not implemented") // we don't care about this function for this test
}

func (t *testNetwork) testResponse(times uint8, callback func(), response []byte) {
	t.response = make([][]byte, times)
	for i := uint8(0); i < times; i++ {
		t.response[i] = response
	}
	t.callback = callback
	t.numCalls = 0
}

func (t *testNetwork) testResponses(callback func(), responses ...[]byte) {
	t.response = responses
	t.callback = callback
	t.numCalls = 0
}

func (t *testNetwork) TrackBandwidth(ids.NodeID, float64) {}
