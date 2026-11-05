// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nonvalidator

import (
	"bytes"
	"sync"
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// messageQueue keeps a queue of messages
type messageQueue struct {
	responsesLock sync.Mutex
	responses     []*messageInfo
}

func (m *messageQueue) clearResponses() {
	m.responsesLock.Lock()
	defer m.responsesLock.Unlock()
	m.responses = []*messageInfo{}
}

func (m *messageQueue) enqueue(mi *messageInfo) {
	m.responsesLock.Lock()
	defer m.responsesLock.Unlock()
	m.responses = append(m.responses, mi)
}

func (m *messageQueue) popResponse() (*messageInfo, bool) {
	m.responsesLock.Lock()
	defer m.responsesLock.Unlock()
	if len(m.responses) == 0 {
		return nil, false
	}
	msg := m.responses[0]
	m.responses = m.responses[1:]
	return msg, true
}

// routerComm appends messages being sent or broadcast to the message queue.
type routerComm struct {
	t *testing.T

	nodes common.Nodes

	// ID is the sender of messages and is the Node using the struct.
	ID common.NodeID

	messageQueue *messageQueue
}

type testEpochs struct {
	t      *testing.T
	epochs []*simplex.Epoch
}

func newTestEpochs(tc *testChain, msgQueue *messageQueue, maxSeqWindow uint64) *testEpochs {
	nodes := tc.nodes()
	epochs := make([]*simplex.Epoch, 0, len(nodes))

	for _, node := range nodes {
		epochNodeID := node.Id

		comm := &routerComm{
			nodes:        nodes,
			t:            tc.t,
			ID:           epochNodeID,
			messageQueue: msgQueue,
		}

		conf, _, _ := testutil.DefaultTestNodeEpochConfig(tc.t, epochNodeID, comm, testutil.NewTestBlockBuilder())
		conf.MaxRoundWindow = maxSeqWindow
		conf.Storage = tc
		conf.SignatureAggregatorCreator = tc.signatureAggregatorCreator
		conf.ReplicationEnabled = true

		epoch, err := simplex.NewEpoch(conf)
		require.NoError(tc.t, err)

		epochs = append(epochs, epoch)
	}

	return &testEpochs{
		t:      tc.t,
		epochs: epochs,
	}
}

// start starts every epoch in the network, failing the test if any epoch
// fails to start.
func (e *testEpochs) start() {
	for _, epoch := range e.epochs {
		require.NoError(e.t, epoch.Start())
	}
}

// stop stops every epoch in the network.
func (e *testEpochs) stop() {
	for _, epoch := range e.epochs {
		epoch.Stop()
	}
}

// handleMessage routes messages between the non-validator and its epochs: a request
// originating from the non-validator is delivered to the addressed epoch, while
// a response from an epoch is delivered back to the non-validator.
func handleMessage(epochs *testEpochs, nv *NonValidator, mi *messageInfo) {
	if !bytes.Equal(mi.from, nv.ID) {
		// a response from an epoch: deliver it to the non-validator.
		require.NoError(epochs.t, nv.HandleMessage(mi.msg, mi.from))
		return
	}

	// a request from the non-validator: route it to the addressed epoch.
	for _, epoch := range epochs.epochs {
		if bytes.Equal(epoch.ID, mi.to) {
			require.NoError(epochs.t, epoch.HandleMessage(mi.msg, mi.from))
			return
		}
	}
	require.Failf(epochs.t, "no epoch for destination", "destination %x", mi.to)
}

func (r *routerComm) Validators() common.Nodes { return r.nodes }

// Enqueues a message sent from this node to `destination`.
func (r *routerComm) Send(msg *common.Message, destination common.NodeID) {
	r.handle(msg, destination)
}

// Enqueues a copy of the message addressed to every other node in the network.
func (r *routerComm) Broadcast(msg *common.Message) {
	for _, n := range r.nodes {
		if bytes.Equal(n.Id, r.ID) {
			continue
		}

		r.handle(msg, n.Id)
	}
}

func (r *routerComm) handle(msg *common.Message, to common.NodeID) {
	switch {
	case msg.VerifiedReplicationResponse != nil:
		// Outgoing responses are of the verified type, but incoming responses
		// are of the unverified type, so we translate before enqueuing.
		vrr := msg.VerifiedReplicationResponse
		data := make([]common.QuorumRound, 0, len(vrr.Data))
		for _, vqr := range vrr.Data {
			data = append(data, *verifiedQRtoQR(&vqr))
		}

		msg = &common.Message{
			ReplicationResponse: &common.ReplicationResponse{
				Data:        data,
				LatestRound: verifiedQRtoQR(vrr.LatestRound),
				LatestSeq:   verifiedQRtoQR(vrr.LatestFinalizedSeq),
			},
		}
		r.messageQueue.enqueue(&messageInfo{msg: msg, from: r.ID, to: to})
	default:
		r.messageQueue.enqueue(&messageInfo{msg: msg, from: r.ID, to: to})
	}
}

func verifiedQRtoQR(vqr *common.VerifiedQuorumRound) *common.QuorumRound {
	if vqr == nil {
		return nil
	}

	qr := &common.QuorumRound{
		Notarization:      vqr.Notarization,
		Finalization:      vqr.Finalization,
		EmptyNotarization: vqr.EmptyNotarization,
	}

	if vqr.VerifiedBlock != nil {
		qr.Block = vqr.VerifiedBlock.(common.Block)
	}

	return qr
}
