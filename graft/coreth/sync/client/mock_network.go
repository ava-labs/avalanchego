// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/peer"

	"github.com/ava-labs/avalanchego/version"
)

var _ peer.NetworkClient = &mockNetwork{}

// TODO replace with gomock library
type mockNetwork struct {
	// captured request data
	numCalls         uint
	requestedVersion *version.Application
	request          []byte

	// response mocking for RequestAny and Request calls
	response       [][]byte
	callback       func() // callback is called prior to processing each mock call
	requestErr     []error
	nodesRequested []ids.NodeID
}

func (t *mockNetwork) SendAppRequestAny(ctx context.Context, minVersion *version.Application, request []byte) ([]byte, ids.NodeID, error) {
	if len(t.response) == 0 {
		return nil, ids.EmptyNodeID, errors.New("no mocked response to return in mockNetwork")
	}

	t.requestedVersion = minVersion

	response, err := t.processMock(request)
	return response, ids.EmptyNodeID, err
}

func (t *mockNetwork) SendAppRequest(ctx context.Context, nodeID ids.NodeID, request []byte) ([]byte, error) {
	if len(t.response) == 0 {
		return nil, errors.New("no mocked response to return in mockNetwork")
	}

	t.nodesRequested = append(t.nodesRequested, nodeID)

	return t.processMock(request)
}

func (t *mockNetwork) processMock(request []byte) ([]byte, error) {
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

func (t *mockNetwork) Gossip([]byte) error {
	panic("not implemented") // we don't care about this function for this test
}

func (t *mockNetwork) mockResponse(times uint8, callback func(), response []byte) {
	t.response = make([][]byte, times)
	for i := uint8(0); i < times; i++ {
		t.response[i] = response
	}
	t.callback = callback
	t.numCalls = 0
}

func (t *mockNetwork) mockResponses(callback func(), responses ...[]byte) {
	t.response = responses
	t.callback = callback
	t.numCalls = 0
}

func (t *mockNetwork) TrackBandwidth(ids.NodeID, float64) {}
