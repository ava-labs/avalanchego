// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"sync/atomic"
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

type randomNodeConfig struct {
	mempool *Mempool
	storage *Storage
	wal     *testutil.TestWAL
	logger  *testutil.TestLogger
}

type Node struct {
	*testutil.BasicNode

	storage *Storage
	wal     *testutil.TestWAL
	mempool *Mempool

	logger    *testutil.TestLogger
	isCrashed atomic.Bool
}

func NewNode(t *testing.T, nodeID common.NodeID, net *testutil.BasicInMemoryNetwork, config *FuzzConfig, nodeConfig randomNodeConfig) *Node {
	var l *testutil.TestLogger
	if nodeConfig.logger != nil {
		l = nodeConfig.logger
	} else {
		l = CreateNodeLogger(t, config, nodeID)
	}

	var mempool *Mempool

	if nodeConfig.mempool != nil {
		mempool = nodeConfig.mempool
	} else {
		mempool = NewMempool(l, config)
	}

	comm := testutil.NewTestComm(nodeID, net, testutil.AllowAllMessages)
	epochConfig, wal, _ := testutil.DefaultTestNodeEpochConfig(t, nodeID, comm, mempool)
	epochConfig.Logger = l
	epochConfig.MaxRoundWindow = 100
	epochConfig.ReplicationEnabled = true

	// storage
	var storage *Storage
	if nodeConfig.storage != nil {
		storage = nodeConfig.storage
	} else {
		storage = NewStorage(mempool)
	}
	epochConfig.Storage = storage

	// wal
	if nodeConfig.wal != nil {
		wal = nodeConfig.wal
	}
	epochConfig.WAL = wal

	epochConfig.BlockDeserializer = &BlockDeserializer{
		mempool: mempool,
	}

	e, err := simplex.NewEpoch(epochConfig)
	require.NoError(t, err)

	n := &Node{
		BasicNode: testutil.NewBasicNode(t, e, l),
		storage:   storage,
		mempool:   mempool,
		logger:    l,
		wal:       wal,
		isCrashed: atomic.Bool{},
	}

	n.CustomHandler = n.HandleMessage

	return n
}

func (n *Node) HandleMessage(msg *common.Message, from common.NodeID) error {
	msgCopy := n.copyMessage(msg)
	return n.BasicNode.HandleMessage(&msgCopy, from)
}

// copyBlock creates a copy of a simplex.Block with the node's mempool.
func (n *Node) copyBlock(b common.Block) common.Block {
	blockCopy := *b.(*Block)
	blockCopy.mempool = n.mempool
	return &blockCopy
}

// copyMessage creates a copy of the message and its relevant fields to avoid mutating shared state in the in-memory network
// this is important because blocks are not serialized/deserialized in our current comm implementation, so sending blocks
// also sends relevant state associated with the node that is sending the message which can cause unintended side effects.
func (n *Node) copyMessage(msg *common.Message) common.Message {
	msgCopy := *msg

	switch {
	case msgCopy.BlockMessage != nil:
		blockMsgCopy := *msgCopy.BlockMessage
		blockMsgCopy.Block = n.copyBlock(msgCopy.BlockMessage.Block)
		msgCopy.BlockMessage = &blockMsgCopy

	case msgCopy.ReplicationResponse != nil:
		rrCopy := *msgCopy.ReplicationResponse

		// Also copy the Data slice to avoid mutating shared slice
		rrCopy.Data = make([]common.QuorumRound, len(msgCopy.ReplicationResponse.Data))
		copy(rrCopy.Data, msgCopy.ReplicationResponse.Data)
		msgCopy.ReplicationResponse = &rrCopy

		// convert quorum rounds to our type
		for i, qr := range msgCopy.ReplicationResponse.Data {
			if qr.Block != nil {
				msgCopy.ReplicationResponse.Data[i].Block = n.copyBlock(qr.Block)
			}
		}

		if msgCopy.ReplicationResponse.LatestRound != nil {
			latestRoundCopy := *msgCopy.ReplicationResponse.LatestRound
			msgCopy.ReplicationResponse.LatestRound = &latestRoundCopy
			if latestRoundCopy.Block != nil {
				msgCopy.ReplicationResponse.LatestRound.Block = n.copyBlock(latestRoundCopy.Block)
			}
		}

		if msgCopy.ReplicationResponse.LatestSeq != nil {
			latestSeqCopy := *msgCopy.ReplicationResponse.LatestSeq
			msgCopy.ReplicationResponse.LatestSeq = &latestSeqCopy
			if latestSeqCopy.Block != nil {
				msgCopy.ReplicationResponse.LatestSeq.Block = n.copyBlock(latestSeqCopy.Block)
			}
		}
	default:
		// no-op
	}
	return msgCopy
}

func (n *Node) areTxsAccepted(txs []*TX) bool {
	n.mempool.lock.Lock()
	defer n.mempool.lock.Unlock()

	for _, tx := range txs {
		if _, exists := n.mempool.acceptedTXs[tx.ID]; !exists {
			return false
		}
	}
	return true
}
