// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

func rejectReplicationRequests(msg *common.Message, _, _ common.NodeID) bool {
	return msg.ReplicationRequest == nil && msg.ReplicationResponse == nil && msg.VerifiedReplicationResponse == nil
}

// A node attempts to request blocks to replicate, but fails to receive them
func TestReplicationRequestTimeout(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}
	numInitialSeqs := uint64(8)

	// node begins replication
	net := testutil.NewControlledNetwork(t, nodes)

	storageData := createBlocks(t, nodes, numInitialSeqs)

	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, rejectReplicationRequests)
		return &testutil.TestNodeConfig{
			InitialStorage:     storageData,
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	})

	net.StartInstances()
	defer net.StopInstances()
	net.TriggerLeaderBlockBuilder(0)

	// typically the lagging node would catch up here, but since we block
	// replication requests, the lagging node will be forced to resend requests after a timeout
	for i := uint64(0); i <= numInitialSeqs; i++ {
		for _, n := range net.Instances {
			if n.E.ID.Equals(laggingNode.E.ID) {
				continue
			}
			n.Storage.WaitForBlockCommit(i)
		}
	}

	// assert the lagging node has not received any replication requests
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	// allow the replication state to cancel the request before setting filter
	time.Sleep(100 * time.Millisecond)
	// after the timeout, the nodes should respond and the lagging node will replicate
	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout / 2))
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout * 2))
	laggingNode.Storage.WaitForBlockCommit(uint64(numInitialSeqs))
}

type testTimeoutMessageFilter struct {
	t *testing.T

	replicationResponses chan struct{}
}

func (m *testTimeoutMessageFilter) failOnReplicationRequest(msg *common.Message, _, _ common.NodeID) bool {
	require.Nil(m.t, msg.ReplicationRequest)
	return true
}

// receiveReplicationRequest is used to filter out sending replication responses, and notify a channel
// when a replication request is received.
func (m *testTimeoutMessageFilter) receivedReplicationRequest(msg *common.Message, _, _ common.NodeID) bool {
	if msg.VerifiedReplicationResponse != nil || msg.ReplicationResponse != nil {
		m.replicationResponses <- struct{}{}
		return false
	}

	return true
}

func TestReplicationRequestTimeoutCancels(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}
	startSeq := uint64(8)

	net := testutil.NewControlledNetwork(t, nodes)

	// initiate a network with 4 nodes. one node is behind by startSeq blocks
	storageData := createBlocks(t, nodes, startSeq)
	testEpochConfig := &testutil.TestNodeConfig{
		InitialStorage:     storageData,
		ReplicationEnabled: true,
	}
	testutil.NewControlledSimplexNode(t, nodes[0], net, testEpochConfig)
	testutil.NewControlledSimplexNode(t, nodes[1], net, testEpochConfig)
	testutil.NewControlledSimplexNode(t, nodes[2], net, testEpochConfig)
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	})

	net.StartInstances()
	defer net.StopInstances()
	net.TriggerLeaderBlockBuilder(startSeq)

	// all blocks except the lagging node start at round 8, seq 8.
	// lagging node starts at round 0, seq 0.
	// this asserts that the lagging node catches up to the latest round
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(startSeq)
	}

	// ensure lagging node doesn't resend requests
	mf := &testTimeoutMessageFilter{
		t: t,
	}

	// allow the replication state to cancel the request before setting filter
	time.Sleep(100 * time.Millisecond)
	laggingNode.E.Comm.(*testutil.TestComm).SetFilter(mf.failOnReplicationRequest)
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout * 2))

	// ensure enough time passes after advanceTime is called
	net.TriggerLeaderBlockBuilder(startSeq + 1)
	for _, n := range net.Instances {
		n.Storage.WaitForBlockCommit(startSeq + 1)
	}
}

// A node attempts to request blocks to replicate, but fails to
// receive them multiple times
func TestReplicationRequestTimeoutMultiple(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}
	startSeq := uint64(8)

	// node begins replication
	net := testutil.NewControlledNetwork(t, nodes)

	storageData := createBlocks(t, nodes, startSeq)

	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, rejectReplicationRequests)
		return &testutil.TestNodeConfig{
			InitialStorage:     storageData,
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	mf := &testTimeoutMessageFilter{
		t:                    t,
		replicationResponses: make(chan struct{}, 1),
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	normalNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	normalNode2.E.Comm.(*testutil.TestComm).SetFilter(mf.receivedReplicationRequest)
	testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
	})

	net.StartInstances()
	defer net.StopInstances()
	net.TriggerLeaderBlockBuilder(0)

	// typically the lagging node would catch up here, but since we block
	// replication requests, the lagging node will be forced to resend requests after a timeout
	for i := 0; i <= int(startSeq); i++ {
		for _, n := range net.Instances {
			if n.E.ID.Equals(laggingNode.E.ID) {
				continue
			}
			n.Storage.WaitForBlockCommit(uint64(startSeq))
		}
	}

	// this is done from normalNode2 since the lagging node will request
	// seqs [0-startSeq/3] after the timeout
	<-mf.replicationResponses

	// assert the lagging node has not received any replication responses
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	normalNode2.E.Comm.(*testutil.TestComm).SetFilter(testutil.AllowAllMessages)

	// after the timeout, only normalNode2 should respond
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout / 2))
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout))
	laggingNode.Storage.WaitForBlockCommit(startSeq / 3)

	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	// timeout again, now all nodes will respond
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout * 2))
	laggingNode.Storage.WaitForBlockCommit(startSeq)
}

// modifies the replication response to only send every even quorum round
func incompleteReplicationResponseFilter(msg *common.Message, _, _ common.NodeID) bool {
	if msg.VerifiedReplicationResponse != nil || msg.ReplicationResponse != nil {
		newLen := len(msg.VerifiedReplicationResponse.Data) / 2
		newData := make([]common.VerifiedQuorumRound, 0, newLen)

		for _, qr := range msg.VerifiedReplicationResponse.Data {
			if qr.GetRound()%2 == 0 {
				newData = append(newData, qr)
			}
		}
		msg.VerifiedReplicationResponse.Data = newData
	}
	return true
}

// A node attempts to request blocks to replicate, but receives incomplete
// responses from nodes.
func TestReplicationRequestIncompleteResponses(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}
	startSeq := uint64(8)

	// node begins replication
	net := testutil.NewControlledNetwork(t, nodes)

	storageData := createBlocks(t, nodes, startSeq)

	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, rejectReplicationRequests)
		return &testutil.TestNodeConfig{
			InitialStorage:     storageData,
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	mf := &testTimeoutMessageFilter{
		t:                    t,
		replicationResponses: make(chan struct{}, 1),
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	normalNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	normalNode2.E.Comm.(*testutil.TestComm).SetFilter(mf.receivedReplicationRequest)
	testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))

	recordedMessages := make(chan *common.Message, 1000)
	comm := testutil.NewTestComm(nodes[3], net.BasicInMemoryNetwork, testutil.AllowAllMessages)

	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
		Comm:               &recordingComm{Communication: comm, SentMessages: recordedMessages},
	})

	for _, node := range net.Instances[:3] {
		node.Silence()
	}

	net.StartInstances()
	defer net.StopInstances()
	net.TriggerLeaderBlockBuilder(startSeq)

	// typically the lagging node would catch up here, but since we block
	// replication requests, the lagging node will be forced to resend requests after a timeout
	for i := 0; i <= int(startSeq); i++ {
		for _, n := range net.Instances {
			if n.E.ID.Equals(laggingNode.E.ID) {
				continue
			}
			n.Storage.WaitForBlockCommit(uint64(startSeq))
		}
	}

	// this is done from normalNode2 since the lagging node will request
	// seqs [0-startSeq/3] after the timeout
	<-mf.replicationResponses

	// assert the lagging node has not received any replication responses
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	net.SetAllNodesMessageFilter(incompleteReplicationResponseFilter)

	// after the timeout, only normalNode2 should respond(but with incomplete data)
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout / 2))
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout))
	laggingNode.Storage.WaitForBlockCommit(0)

	require.Eventually(t, func() bool {
		msg, ok := <-recordedMessages
		if !ok {
			return false
		}

		if msg.ReplicationRequest == nil {
			return false
		}

		return reflect.DeepEqual(msg.ReplicationRequest.Seqs, []uint64{3, 4, 5})

	}, 30*time.Second, 10*time.Millisecond)

	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	// timeout again, now all nodes will respond
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout * 2))
	laggingNode.Storage.WaitForBlockCommit(startSeq)
}

type collectNotarizationComm struct {
	lock          *sync.Mutex
	notarizations map[uint64]*common.Notarization
	testutil.TestNetworkCommunication

	replicationResponses chan struct{}
}

func newCollectNotarizationComm(nodeID common.NodeID, net *testutil.ControlledInMemoryNetwork, notarizations map[uint64]*common.Notarization, lock *sync.Mutex) *collectNotarizationComm {
	return &collectNotarizationComm{
		notarizations:            notarizations,
		TestNetworkCommunication: testutil.NewTestComm(nodeID, net.BasicInMemoryNetwork, testutil.AllowAllMessages),
		replicationResponses:     make(chan struct{}, 3),
		lock:                     lock,
	}
}

func (c *collectNotarizationComm) Send(msg *common.Message, to common.NodeID) {
	if msg.Notarization != nil {
		c.lock.Lock()
		c.notarizations[msg.Notarization.Vote.Round] = msg.Notarization
		c.lock.Unlock()
	}
	c.TestNetworkCommunication.Send(msg, to)
}

func (c *collectNotarizationComm) Broadcast(msg *common.Message) {
	if msg.Notarization != nil {
		c.lock.Lock()
		c.notarizations[msg.Notarization.Vote.Round] = msg.Notarization
		c.lock.Unlock()
	}
	c.TestNetworkCommunication.Broadcast(msg)
}

func (c *collectNotarizationComm) removeFinalizationsFromReplicationResponses(msg *common.Message, from, to common.NodeID) bool {
	c.lock.Lock()
	defer c.lock.Unlock()

	if msg.VerifiedReplicationResponse != nil || msg.ReplicationResponse != nil {
		newData := make([]common.VerifiedQuorumRound, 0, len(msg.VerifiedReplicationResponse.Data))

		for i := 0; i < len(msg.VerifiedReplicationResponse.Data); i++ {
			qr := msg.VerifiedReplicationResponse.Data[i]
			if qr.Finalization != nil && c.notarizations[qr.GetRound()] != nil {
				qr.Finalization = nil
				qr.Notarization = c.notarizations[qr.GetRound()]
			}
			newData = append(newData, qr)
		}
		msg.VerifiedReplicationResponse.Data = newData
		select {
		case c.replicationResponses <- struct{}{}:
		default:
		}
	}

	if msg.Finalization != nil && msg.Finalization.Finalization.Round == 0 {
		// we drop a finalization here because the lagging node could timeout on round 0
		// therefore it would send an empty vote message to a node.
		// When nodes receive empty votes, they send back a finalization/notarization to a lagging node.
		// Since we are testing replication, lets block this finalization to ensure the lagging node
		// has to rely on replication to get notarizations/finalizations.
		return false
	}

	return true
}

// TestReplicationRequestWithoutFinalization tests that a replication request is not marked as completed
// if we are expecting a finalization but it is not present in the response.
func TestReplicationRequestWithoutFinalization(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}
	endDisconnect := uint64(10)
	net := testutil.NewControlledNetwork(t, nodes)

	notarizations := make(map[uint64]*common.Notarization)
	mapLock := &sync.Mutex{}
	testConfig := func(nodeID common.NodeID) *testutil.TestNodeConfig {
		return &testutil.TestNodeConfig{
			ReplicationEnabled: true,
			Comm:               newCollectNotarizationComm(nodeID, net, notarizations, mapLock),
		}
	}

	notarizationComm := newCollectNotarizationComm(nodes[0], net, notarizations, mapLock)
	testutil.NewControlledSimplexNode(t, nodes[0], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
		Comm:               notarizationComm,
	})
	testutil.NewControlledSimplexNode(t, nodes[1], net, testConfig(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, testConfig(nodes[2]))
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, testConfig(nodes[3]))

	// lagging node disconnects
	net.Disconnect(nodes[3])
	net.StartInstances()
	defer net.StopInstances()

	missedSeqs := uint64(0)
	// normal nodes continue to make progress
	for i := uint64(0); i < endDisconnect; i++ {
		emptyRound := bytes.Equal(simplex.LeaderForRound(nodes, i), nodes[3])
		if emptyRound {
			net.AdvanceWithoutLeader(i, laggingNode.E.ID)
			missedSeqs++
		} else {
			net.TriggerLeaderBlockBuilder(i)
			for _, n := range net.Instances[:3] {
				n.Storage.WaitForBlockCommit(i - missedSeqs)
			}
		}
	}

	// all nodes except for lagging node have progressed and committed [endDisconnect - missedSeqs] blocks
	for _, n := range net.Instances[:3] {
		require.Equal(t, endDisconnect-missedSeqs, n.Storage.NumBlocks())
	}
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	require.Equal(t, uint64(0), laggingNode.E.Metadata().Round)
	// lagging node reconnects
	net.SetAllNodesMessageFilter(notarizationComm.removeFinalizationsFromReplicationResponses)
	net.Connect(nodes[3])
	net.TriggerLeaderBlockBuilder(endDisconnect)
	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			continue
		}

		n.Storage.WaitForBlockCommit(endDisconnect - missedSeqs)
		<-notarizationComm.replicationResponses
	}

	// wait until the lagging nodes sends replication requests
	// due to the removeFinalizationsFromReplicationResponses message filter
	// the lagging node should not have processed any replication requests
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	// we should still have these replication requests in the timeout handler
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout * 2))
	for range net.Instances[:3] {
		// the lagging node should have sent replication requests
		// and the normal nodes should have responded
		<-notarizationComm.replicationResponses
	}
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	// We should still have these replication requests in the timeout handler
	// but now we allow the lagging node to process them
	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)

	timeout := time.After(1 * time.Minute)

	// we may be in the process of creating timeout requests
	for {
		laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout * 4))
		if laggingNode.Storage.NumBlocks() > endDisconnect-missedSeqs {
			break
		}

		select {
		case <-time.After(100 * time.Millisecond):
			continue
		case <-timeout:
			t.Fatalf("Lagging node did not catch up after timeout")
		}
	}
}

// TestReplicationMalformedQuorumRound tests that a node re-sends a replication request when it receives a malformed quorum round message.
func TestReplicationMalformedQuorumRound(t *testing.T) {
	nodes := []common.NodeID{{1}, {2}, {3}, []byte("lagging")}
	startSeq := uint64(8)

	// node begins replication
	net := testutil.NewControlledNetwork(t, nodes)

	storageData := createBlocks(t, nodes, startSeq)

	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, rejectReplicationRequests)
		return &testutil.TestNodeConfig{
			InitialStorage:     storageData,
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	mf := &testTimeoutMessageFilter{
		t:                    t,
		replicationResponses: make(chan struct{}, 1),
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	normalNode2 := testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	normalNode2.E.Comm.(*testutil.TestComm).SetFilter(mf.receivedReplicationRequest)
	testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))

	recordedMessages := make(chan *common.Message, 1000)
	comm := testutil.NewTestComm(nodes[3], net.BasicInMemoryNetwork, testutil.AllowAllMessages)

	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
		Comm:               &recordingComm{Communication: comm, SentMessages: recordedMessages},
	})

	net.StartInstances()
	defer net.StopInstances()
	net.TriggerLeaderBlockBuilder(0)

	// typically the lagging node would catch up here, but since we block
	// replication requests, the lagging node will be forced to resend requests after a timeout
	for i := 0; i <= int(startSeq); i++ {
		for _, n := range net.Instances {
			if n.E.ID.Equals(laggingNode.E.ID) {
				continue
			}
			n.Storage.WaitForBlockCommit(uint64(startSeq))
		}
	}

	<-mf.replicationResponses

	// assert the lagging node has not received any replication responses
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	net.SetAllNodesMessageFilter(
		func(msg *common.Message, _, _ common.NodeID) bool {
			if msg.VerifiedReplicationResponse != nil || msg.ReplicationResponse != nil {
				newData := make([]common.VerifiedQuorumRound, 0, len(msg.VerifiedReplicationResponse.Data))

				for _, qr := range msg.VerifiedReplicationResponse.Data {
					qr.Notarization = nil // remove notarization
					qr.Finalization = nil // remove finalization
					newData = append(newData, qr)
				}
				msg.VerifiedReplicationResponse.Data = newData
			}
			return true
		},
	)

	// after the timeout, only normalNode2 should respond, but with malformed data
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout / 2))
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout))
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())

	require.Eventually(t, func() bool {
		msg, ok := <-recordedMessages
		if !ok {
			return false
		}

		if msg.ReplicationRequest == nil {
			return false
		}

		return reflect.DeepEqual(msg.ReplicationRequest.Seqs, []uint64{3, 4, 5})

	}, 30*time.Second, 10*time.Millisecond)

	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	// timeout again, now all nodes will respond
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout * 2))
	laggingNode.Storage.WaitForBlockCommit(startSeq)
}

func TestReplicationResendsFinalizedBlocksThatFailedVerification(t *testing.T) {

	// send a block, then simultaneously send a finalization for the block
	bb := testutil.NewTestBlockBuilder()

	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	quorum := common.Quorum(len(nodes))
	sentMessages := make(chan *common.Message, 100)

	conf, _, storage := testutil.DefaultTestNodeEpochConfig(t, nodes[1], &recordingComm{
		Communication: testutil.NewNoopComm(nodes),
		SentMessages:  sentMessages,
	}, bb)

	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := e.Metadata()
	_, ok := bb.BuildBlock(context.Background(), md, emptyBlacklist)
	require.True(t, ok)
	require.Equal(t, md.Round, md.Seq)

	block := bb.GetBuiltBlock()
	block.VerificationError = errors.New("block verification failed")

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	finalization, _ := testutil.NewFinalizationRecord(t, sigAggr, block, nodes[0:quorum])

	// send the finalization to start the replication process
	require.NoError(t, e.HandleMessage(&common.Message{
		Finalization: &finalization,
	}, nodes[0]))
	// wait for the replication request to be sent
	for {
		msg := <-sentMessages
		if msg.ReplicationRequest != nil {
			break
		}
	}
	replicationResponse := &common.ReplicationResponse{
		Data: []common.QuorumRound{
			{
				Block:        block,
				Finalization: &finalization,
			},
		},
	}
	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[0]))
	// wait for the replication request to be sent again
	for {
		msg := <-sentMessages
		if msg.ReplicationRequest != nil {
			break
		}
	}

	block = testutil.NewTestBlock(md, emptyBlacklist)
	block.Data = append(block.Data, 0)
	block.ComputeDigest()

	finalization, _ = testutil.NewFinalizationRecord(t, sigAggr, block, nodes[0:quorum])
	replicationResponse = &common.ReplicationResponse{
		Data: []common.QuorumRound{
			{
				Block:        block,
				Finalization: &finalization,
			},
		},
	}

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: replicationResponse,
	}, nodes[0]))

	storedBlock := storage.WaitForBlockCommit(0)
	require.Equal(t, uint64(1), storage.NumBlocks())
	require.Equal(t, block, storedBlock)
}

// TestReplicationResendsNotarizedBlocksThatFailedVerification ensures a replicated notarized block
// that fails verification causes its round to be re-requested, and a valid block from the
// next response is stored with its notarization.
func TestReplicationResendsNotarizedBlocksThatFailedVerification(t *testing.T) {
	bb := testutil.NewTestBlockBuilder()

	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	quorum := common.Quorum(len(nodes))
	sentMessages := make(chan *common.Message, 100)

	conf, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodes[1], &recordingComm{
		Communication: testutil.NewNoopComm(nodes),
		SentMessages:  sentMessages,
	}, bb)

	conf.ReplicationEnabled = true

	e, err := simplex.NewEpoch(conf)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.NoError(t, e.Start())

	md := e.Metadata()
	block := testutil.NewTestBlock(md, emptyBlacklist)
	block.VerificationError = errors.New("block verification failed")

	sigAggr := e.SignatureAggregatorCreator(conf.Comm.Validators())
	notarization, err := testutil.NewNotarization(conf.Logger, sigAggr, block, nodes[0:quorum])
	require.NoError(t, err)

	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			Data: []common.QuorumRound{{Block: block, Notarization: &notarization}},
		},
	}, nodes[0]))

	// the failed verification re-requests the round, otherwise this blocks forever
	msg := <-sentMessages
	require.Equal(t, []uint64{0}, msg.ReplicationRequest.Rounds)

	block = testutil.NewTestBlock(md, emptyBlacklist)
	block.Data = append(block.Data, 0)
	block.ComputeDigest()

	notarization, err = testutil.NewNotarization(conf.Logger, sigAggr, block, nodes[0:quorum])
	require.NoError(t, err)

	// answer the re-request with a valid block, once verified its notarization is written to the WAL
	require.NoError(t, e.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			Data: []common.QuorumRound{{Block: block, Notarization: &notarization}},
		},
	}, nodes[0]))

	wal.AssertNotarization(0)
}

func TestReplicationResendSplitsRequests(t *testing.T) {
	// ensures that when replication requests time out, the missing sequences
	// are re-requested split across the nodes that signed the highest observed quorum, just like the initial requests are.
	nodes := []common.NodeID{{1}, {2}, {3}, {4}}
	startSeq := 2 * uint64(simplex.DefaultMaxRoundWindow)

	net := testutil.NewControlledNetwork(t, nodes)
	storageData := createBlocks(t, nodes, startSeq)

	newNodeConfig := func(from common.NodeID) *testutil.TestNodeConfig {
		comm := testutil.NewTestComm(from, net.BasicInMemoryNetwork, rejectReplicationRequests)
		return &testutil.TestNodeConfig{
			InitialStorage:     storageData,
			Comm:               comm,
			ReplicationEnabled: true,
		}
	}

	testutil.NewControlledSimplexNode(t, nodes[0], net, newNodeConfig(nodes[0]))
	testutil.NewControlledSimplexNode(t, nodes[1], net, newNodeConfig(nodes[1]))
	testutil.NewControlledSimplexNode(t, nodes[2], net, newNodeConfig(nodes[2]))
	laggingNode := testutil.NewControlledSimplexNode(t, nodes[3], net, &testutil.TestNodeConfig{
		ReplicationEnabled: true,
		// twice the responder's window: the timed-out sequences span more
		// than a responder is willing to server in a single request
		MaxRoundWindow: startSeq,
	})

	net.StartInstances()
	defer net.StopInstances()
	net.TriggerLeaderBlockBuilder(startSeq)

	// the healthy nodes commit, the lagging node's replciation responses
	// were all dropped, so it is stuck at 0.
	for _, n := range net.Instances {
		if n.E.ID.Equals(laggingNode.E.ID) {
			continue
		}
		n.Storage.WaitForBlockCommit(startSeq)
	}
	require.Equal(t, uint64(0), laggingNode.Storage.NumBlocks())
	// wait to register the timeout before healing
	time.Sleep(100 * time.Millisecond)
	// network healed, responsed are no longer dropped
	net.SetAllNodesMessageFilter(testutil.AllowAllMessages)
	// trigger the resend of all timed-out responses. If the resend path doesn't split/ doesn't account for maxRoundWindow
	// this test would hang forever because the responses would be dropped silently.
	laggingNode.E.AdvanceTime(laggingNode.E.StartTime.Add(simplex.DefaultReplicationRequestTimeout * 2))
	laggingNode.Storage.WaitForBlockCommit(startSeq)
}
