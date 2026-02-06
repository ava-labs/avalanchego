// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/version"
)

var _ Network = (*testNetwork)(nil)

type testNetwork struct {
	// captured request data
	numCalls uint

	// response testing for RequestAny and Request calls
	response       [][]byte
	callback       func() // callback is called prior to processing each test call
	requestErr     []error
	nodesRequested []ids.NodeID
}

func (*testNetwork) P2PNetwork() *p2p.Network {
	panic("P2PNetwork unimplemented")
}

func (*testNetwork) P2PValidators() *p2p.Validators {
	panic("P2PValidators unimplemented")
}

func (t *testNetwork) SendSyncedAppRequestAny(_ context.Context, _ *version.Application, _ []byte) ([]byte, ids.NodeID, error) {
	if len(t.response) == 0 {
		return nil, ids.EmptyNodeID, errors.New("no tested response to return in testNetwork")
	}

	response, err := t.processTest()
	return response, ids.EmptyNodeID, err
}

func (t *testNetwork) SendSyncedAppRequest(_ context.Context, nodeID ids.NodeID, _ []byte) ([]byte, error) {
	if len(t.response) == 0 {
		return nil, errors.New("no tested response to return in testNetwork")
	}

	t.nodesRequested = append(t.nodesRequested, nodeID)

	return t.processTest()
}

func (t *testNetwork) processTest() ([]byte, error) {
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

func (*testNetwork) TrackBandwidth(ids.NodeID, float64) {}
