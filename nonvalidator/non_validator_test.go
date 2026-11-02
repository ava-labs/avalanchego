// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nonvalidator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// errQC is a QuorumCertificate whose Verify always fails. Used to drive
// the "qc does not verify" code path without mutating message contents.
type errQC struct {
	common.QuorumCertificate
}

func (errQC) Verify(_ []byte, _ common.Nodes) error {
	return errors.New("qc verification failed")
}

func blockMessage(t *testing.T, block common.Block, from common.NodeID) *common.Message {
	vote, err := testutil.NewTestVote(block, from)
	require.NoError(t, err)
	return &common.Message{
		BlockMessage: &common.BlockMessage{
			Block: block,
			Vote:  *vote,
		},
	}
}

func newBlock(seq, epoch uint64, prev common.Digest) *testutil.TestBlock {
	return testutil.NewTestBlock(common.ProtocolMetadata{
		Round: seq,
		Seq:   seq,
		Epoch: epoch,
		Prev:  prev,
	}, common.Blacklist{})
}

// blockMsg wraps b in a messageInfo sent by the round leader.
func blockMsg(t *testing.T, b common.Block, nodes common.Nodes) *messageInfo {
	leader := simplex.LeaderForRound(nodes.NodeIDs(), b.BlockHeader().Round)
	return &messageInfo{
		msg:  blockMessage(t, b, leader),
		from: leader,
	}
}

// finalizationMsg mints a fresh Finalization for b signed by every node in
// nodes, and wraps it in a messageInfo sent by nodes[0]. It takes the block
// (not a pre-built finalization) so callers can chain straight from
// chain.appendBlock without keeping a separate finalization variable.
func finalizationMsg(t *testing.T, b common.VerifiedBlock, nodes common.Nodes) *messageInfo {
	fin, _ := testutil.NewFinalizationRecord(t, &testutil.TestSignatureAggregator{}, b, nodes.NodeIDs())
	return &messageInfo{
		msg:  &common.Message{Finalization: &fin},
		from: nodes.NodeIDs()[0],
	}
}

// TestHandleMessages drives the non-validator with blocks and finalizations
// arriving in various orders and asserts it verifies and indexes them up to
// the expected height, including across epoch transitions.
func TestHandleMessages(t *testing.T) {
	// Each case starts from a chain seeded with seq 0 (genesis), seq 1
	// (sealing block opening epoch 1), and seq 2 (an epoch-1 block).
	tests := []struct {
		name           string
		setup          func(t *testing.T) (*testChain, []*messageInfo)
		expectedHeight uint64
	}{
		{
			name: "blocks and finalizations out of order",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3, b4, b5 := tc.appendBlock(), tc.appendBlock(), tc.appendBlock()

				// finalization for seq 3 arrives last and should kick off
				// verification for seqs 3-5.
				return tc, []*messageInfo{
					blockMsg(t, b4, testNodes),
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b4, testNodes),
					finalizationMsg(t, b5, testNodes),
					blockMsg(t, b5, testNodes),
					finalizationMsg(t, b3, testNodes),
				}
			},
			expectedHeight: 6,
		},
		{
			name: "nil block doesn't panic",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				msg := blockMsg(t, b3, testNodes)
				msg.msg.BlockMessage.Block = nil

				return tc, []*messageInfo{msg}
			},
			expectedHeight: 3,
		},
		{
			name: "same block doesn't get verified or indexed twice",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3, b4, b5 := tc.appendBlock(), tc.appendBlock(), tc.appendBlock()

				// sending a second b4 message will try and schedule the verification task for seq 4 again
				// If the task is scheduled twice, the test will fail since storage will panic if we index the same seq twice.
				return tc, []*messageInfo{
					blockMsg(t, b4, testNodes),
					finalizationMsg(t, b4, testNodes),
					blockMsg(t, b4, testNodes),
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b5, testNodes),
					blockMsg(t, b5, testNodes),
					finalizationMsg(t, b3, testNodes),
				}
			},
			expectedHeight: 6,
		},
		{
			name: "next block followed by finalization",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()

				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
				}
			},
			expectedHeight: 4,
		},
		{
			name: "block message received from non leader",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()

				blockMsg := blockMsg(t, b3, testNodes)
				blockMsg.from = testNodes.NodeIDs()[0]
				return tc, []*messageInfo{
					blockMsg,
					finalizationMsg(t, b3, testNodes),
				}
			},
			expectedHeight: 3,
		},
		{
			name: "block digest mismatch with finalization",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				f3 := finalizationMsg(t, b3, testNodes)

				b3.Digest = common.Digest{}
				blockMsg := blockMsg(t, b3, testNodes)
				return tc, []*messageInfo{
					blockMsg,
					f3,
				}
			},
			expectedHeight: 3,
		},
		{
			name: "adversary sends block for same sequence as real block",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				junk := testutil.NewTestBlock(common.ProtocolMetadata{
					Round: b3.Metadata.Round + uint64(len(testNodes)),
					Seq:   b3.Metadata.Seq,
					Epoch: b3.Metadata.Epoch,
					Prev:  b3.Metadata.Prev,
				}, common.Blacklist{})

				return tc, []*messageInfo{
					blockMsg(t, junk, testNodes),
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
				}
			},
			expectedHeight: 4,
		},
		{
			name: "junk block then finalization then real block",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				junk := testutil.NewTestBlock(common.ProtocolMetadata{
					Round: b3.Metadata.Round + uint64(len(testNodes)),
					Seq:   b3.Metadata.Seq,
					Epoch: b3.Metadata.Epoch,
					Prev:  b3.Metadata.Prev,
				}, common.Blacklist{})

				return tc, []*messageInfo{
					blockMsg(t, junk, testNodes),
					finalizationMsg(t, b3, testNodes),
					blockMsg(t, b3, testNodes),
				}
			},
			expectedHeight: 4,
		},
		{
			name: "real block then junk block then finalization",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				junk := testutil.NewTestBlock(common.ProtocolMetadata{
					Round: b3.Metadata.Round + uint64(len(testNodes)),
					Seq:   b3.Metadata.Seq,
					Epoch: b3.Metadata.Epoch,
					Prev:  b3.Metadata.Prev,
				}, common.Blacklist{})

				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					blockMsg(t, junk, testNodes),
					// we only store the latest block received per sequence, so junk block overrides b3
					// meaning the height should not advance.
					finalizationMsg(t, b3, testNodes),
				}
			},
			expectedHeight: 3,
		},
		{
			name: "real block then finalization then junk block",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				junk := testutil.NewTestBlock(common.ProtocolMetadata{
					Round: b3.Metadata.Round + uint64(len(testNodes)),
					Seq:   b3.Metadata.Seq,
					Epoch: b3.Metadata.Epoch,
					Prev:  b3.Metadata.Prev,
				}, common.Blacklist{})

				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					blockMsg(t, junk, testNodes),
				}
			},
			expectedHeight: 4,
		},
		{
			name: "finalization then junk block then real block",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				junk := testutil.NewTestBlock(common.ProtocolMetadata{
					Round: b3.Metadata.Round + uint64(len(testNodes)),
					Seq:   b3.Metadata.Seq,
					Epoch: b3.Metadata.Epoch,
					Prev:  b3.Metadata.Prev,
				}, common.Blacklist{})

				return tc, []*messageInfo{
					finalizationMsg(t, b3, testNodes),
					blockMsg(t, junk, testNodes),
					blockMsg(t, b3, testNodes),
				}
			},
			expectedHeight: 4,
		},
		{
			name: "finalization then real block then junk block",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				junk := testutil.NewTestBlock(common.ProtocolMetadata{
					Round: b3.Metadata.Round + uint64(len(testNodes)),
					Seq:   b3.Metadata.Seq,
					Epoch: b3.Metadata.Epoch,
					Prev:  b3.Metadata.Prev,
				}, common.Blacklist{})

				return tc, []*messageInfo{
					finalizationMsg(t, b3, testNodes),
					blockMsg(t, b3, testNodes),
					blockMsg(t, junk, testNodes),
				}
			},
			expectedHeight: 4,
		},
		{
			name: "qc does not verify",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendBlock()
				f3 := finalizationMsg(t, b3, testNodes)
				f3.msg.Finalization.QC = errQC{QuorumCertificate: f3.msg.Finalization.QC}

				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					f3,
				}
			},
			expectedHeight: 3,
		},
		{
			name: "multiple epochs",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)

				var epoch3Nodes = common.Nodes{
					{Id: common.NodeID{1}, Weight: 1},
					{Id: common.NodeID{2}, Weight: 1},
					{Id: common.NodeID{3}, Weight: 1},
					{Id: common.NodeID{4}, Weight: 1},
					{Id: common.NodeID{5}, Weight: 1},
				}

				// Send the sealing block + finalization that transitions to epoch 3.
				b3 := tc.appendSealing(epoch3Nodes)
				// Blocks 4 and 5 live in epoch 3 and are finalized by the new validator set.
				b4, b5 := tc.appendBlock(), tc.appendBlock()
				// Block 6 is another sealing epoch
				b6 := tc.appendSealing(testNodes)
				// Block 7 is part of the new epoch
				b7 := tc.appendBlock()

				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					finalizationMsg(t, b4, epoch3Nodes),
					finalizationMsg(t, b5, epoch3Nodes),
					blockMsg(t, b4, epoch3Nodes),
					blockMsg(t, b5, epoch3Nodes),
					blockMsg(t, b6, epoch3Nodes),
					finalizationMsg(t, b6, epoch3Nodes),
					blockMsg(t, b7, testNodes),
					finalizationMsg(t, b7, testNodes),
				}
			},
			expectedHeight: 8,
		},
		{
			name: "telock finalization after the sealing block is dropped",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				epoch3Nodes := common.Nodes{
					{Id: common.NodeID{1}, Weight: 1},
					{Id: common.NodeID{2}, Weight: 1},
					{Id: common.NodeID{3}, Weight: 1},
					{Id: common.NodeID{4}, Weight: 1},
					{Id: common.NodeID{5}, Weight: 1},
				}

				b3 := tc.appendSealing(epoch3Nodes)
				// The Telock extends epoch 1 past the sealing block and shares seq 4 with the first block of epoch 3.
				telock := newBlock(4, 1, b3.Digest)
				b4 := tc.appendBlock()

				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					finalizationMsg(t, telock, testNodes),
					finalizationMsg(t, b4, epoch3Nodes),
					blockMsg(t, b4, epoch3Nodes),
				}
			},
			expectedHeight: 5,
		},
		{
			name: "telock finalization before the sealing block is dropped",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				epoch3Nodes := common.Nodes{
					{Id: common.NodeID{1}, Weight: 1},
					{Id: common.NodeID{2}, Weight: 1},
					{Id: common.NodeID{3}, Weight: 1},
					{Id: common.NodeID{4}, Weight: 1},
					{Id: common.NodeID{5}, Weight: 1},
				}

				b3 := tc.appendSealing(epoch3Nodes)
				telock := newBlock(4, 1, b3.Digest)
				b4 := tc.appendBlock()

				// The Telock finalization is stored before epoch 3 is known and must be purged once it is.
				return tc, []*messageInfo{
					finalizationMsg(t, telock, testNodes),
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					finalizationMsg(t, b4, epoch3Nodes),
					blockMsg(t, b4, epoch3Nodes),
				}
			},
			expectedHeight: 5,
		},
		{
			name: "telock block before the sealing block is dropped",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				epoch3Nodes := common.Nodes{
					{Id: common.NodeID{1}, Weight: 1},
					{Id: common.NodeID{2}, Weight: 1},
					{Id: common.NodeID{3}, Weight: 1},
					{Id: common.NodeID{4}, Weight: 1},
					{Id: common.NodeID{5}, Weight: 1},
				}

				b3 := tc.appendSealing(epoch3Nodes)
				telock := newBlock(4, 1, b3.Digest)
				b4 := tc.appendBlock()

				// A stored Telock block would otherwise make the epoch 3 block at seq 4 look like a duplicate.
				return tc, []*messageInfo{
					blockMsg(t, telock, testNodes),
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					finalizationMsg(t, b4, epoch3Nodes),
					blockMsg(t, b4, epoch3Nodes),
				}
			},
			expectedHeight: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc, msgs := tt.setup(t)
			require.Equal(t, uint64(3), tc.NumBlocks())
			nv, err := NewNonValidator(
				Config{
					Storage:                    tc,
					Comm:                       testutil.NewNoopComm(tc.nodes().NodeIDs()),
					Logger:                     testutil.MakeLogger(t, 1),
					SignatureAggregatorCreator: tc.signatureAggregatorCreator,
					MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
					ID:                         testNodes[0].Id,
					EpochsReplicated:           true,
				},
			)
			require.NoError(t, err)
			defer nv.Stop()

			for _, m := range msgs {
				require.NoError(t, nv.HandleMessage(m.msg, m.from))
			}

			tc.WaitForBlockCommit(tt.expectedHeight - 1)
			require.Equal(t, tt.expectedHeight, nv.Storage.NumBlocks())
		})
	}
}

// TestNonValidatorDropsTelockQuorumRound replicates a Telock and the next epoch's block
// for the same seq in both orders and asserts the next epoch's block is the one indexed.
func TestNonValidatorDropsTelockQuorumRound(t *testing.T) {
	tests := []struct {
		name        string
		telockFirst bool
	}{
		{name: "telock quorum round first", telockFirst: true},
		{name: "telock quorum round last", telockFirst: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := newSeededChain(t, testNodes, 2)
			epoch3Nodes := common.Nodes{
				{Id: common.NodeID{1}, Weight: 1},
				{Id: common.NodeID{2}, Weight: 1},
				{Id: common.NodeID{3}, Weight: 1},
				{Id: common.NodeID{4}, Weight: 1},
				{Id: common.NodeID{5}, Weight: 1},
			}

			b3 := tc.appendSealing(epoch3Nodes)
			telock := newBlock(4, 1, b3.Digest)
			b4 := tc.appendBlock()
			f3, fTelock, f4 := tc.newFinalization(b3), tc.newFinalization(telock), tc.newFinalization(b4)

			nv, err := NewNonValidator(Config{
				Storage:                    tc,
				Comm:                       testutil.NewNoopComm(tc.nodes().NodeIDs()),
				Logger:                     testutil.MakeLogger(t, 1),
				SignatureAggregatorCreator: tc.signatureAggregatorCreator,
				MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
				ID:                         testNodes[0].Id,
				EpochsReplicated:           true,
			})
			require.NoError(t, err)
			defer nv.Stop()

			telockQR := common.QuorumRound{Block: telock, Finalization: &fTelock}
			b4QR := common.QuorumRound{Block: b4, Finalization: &f4}
			data := []common.QuorumRound{{Block: b3, Finalization: &f3}, telockQR, b4QR}
			if !tt.telockFirst {
				data = []common.QuorumRound{{Block: b3, Finalization: &f3}, b4QR, telockQR}
			}

			require.NoError(t, nv.HandleMessage(
				&common.Message{ReplicationResponse: &common.ReplicationResponse{Data: data}},
				testNodes.NodeIDs()[1],
			))

			indexed := tc.WaitForBlockCommit(4)
			require.Equal(t, b4.BlockHeader().Digest, indexed.BlockHeader().Digest)
			require.Equal(t, uint64(5), tc.NumBlocks())
		})
	}
}

func TestNonValidator_StopsGracefully(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc,
			Comm:                       testutil.NewNoopComm(tc.nodes().NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         testNodes[0].Id,
			EpochsReplicated:           true,
		},
	)
	require.NoError(t, err)

	nv.Start()
	nv.Stop()

	b3 := tc.appendBlock()
	block := blockMsg(t, b3, testNodes)
	require.NoError(t, nv.HandleMessage(block.msg, block.from))
	fin := finalizationMsg(t, b3, testNodes)
	require.NoError(t, nv.HandleMessage(fin.msg, fin.from))

	require.Never(t,
		func() bool {
			return nv.Storage.NumBlocks() > 3
		},
		2*time.Second,
		50*time.Millisecond,
	)
}

// TestHandleMessages_DuplicateBlock tests that when a duplicate block is received, the block is verified & indexed only once
func TestHandleMessages_DuplicateBlock(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc,
			Comm:                       testutil.NewNoopComm(tc.nodes().NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         testNodes[0].Id,
			EpochsReplicated:           true,
		},
	)
	require.NoError(t, err)

	nv.Start()
	defer nv.Stop()

	// Send the sealing block + finalization that transitions to epoch 3.
	b3 := tc.appendBlock()
	block := blockMsg(t, b3, testNodes)
	require.NoError(t, nv.HandleMessage(block.msg, block.from))
	fin := finalizationMsg(t, b3, testNodes)
	require.NoError(t, nv.HandleMessage(fin.msg, fin.from))

	tc.WaitForBlockCommit(3)

	// Storage will panic if we try indexing the same block twice
	require.NoError(t, nv.HandleMessage(block.msg, block.from))
	require.NoError(t, nv.HandleMessage(fin.msg, fin.from))
}

// TestNonValidator_CallsTransition asserts TransitionToValidator fires exactly when
// an indexed sealing block opens the highest known epoch and our ID is in its new
// validator set.
func TestNonValidator_CallsTransition(t *testing.T) {
	newValidatorID := common.NodeID{5}
	joinedSet := append(slices.Clone(testNodes), common.Node{Id: newValidatorID, Weight: 1})
	otherSet := append(slices.Clone(testNodes), common.Node{Id: common.NodeID{6}, Weight: 1})

	// transitionCall records one TransitionToValidator invocation.
	type transitionCall struct {
		epoch      uint64
		validators common.Nodes
	}

	tests := []struct {
		name         string
		setup        func(t *testing.T) (*testChain, []*messageInfo)
		expectedCall *transitionCall
	}{
		{
			name: "joins the new validator set",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendSealing(joinedSet)
				b4 := tc.appendBlock()
				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					blockMsg(t, b4, joinedSet),
					finalizationMsg(t, b4, joinedSet),
				}
			},
			expectedCall: &transitionCall{epoch: 3, validators: joinedSet},
		},
		{
			name: "not in the new validator set",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendSealing(testNodes)
				b4 := tc.appendBlock()
				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					blockMsg(t, b4, testNodes),
					finalizationMsg(t, b4, testNodes),
				}
			},
		},
		{
			// b3 seals an epoch we are not part of, b4 seals the one we join.
			// b3 never triggers: either epoch 4 is already known, or epoch 3's
			// set does not contain us.
			name: "only the highest known epoch triggers",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendSealing(otherSet)
				b4 := tc.appendSealing(joinedSet)
				b5 := tc.appendBlock()
				return tc, []*messageInfo{
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					blockMsg(t, b4, otherSet),
					finalizationMsg(t, b4, otherSet),
					blockMsg(t, b5, joinedSet),
					finalizationMsg(t, b5, joinedSet),
				}
			},
			expectedCall: &transitionCall{epoch: 4, validators: joinedSet},
		},
		{
			// A threshold of quorum rounds for b5, the highest sealing block,
			// validates epoch 5 before anything indexes. b3 then indexes while a
			// higher epoch is already known, so even though we are in b3's set
			// only b5 triggers the transition.
			name: "highest epoch validated up front",
			setup: func(t *testing.T) (*testChain, []*messageInfo) {
				tc := newSeededChain(t, testNodes, 2)
				b3 := tc.appendSealing(joinedSet)
				b4 := tc.appendSealing(otherSet)
				b5 := tc.appendSealing(joinedSet)
				b6 := tc.appendBlock()

				f5 := tc.newFinalization(b5)
				qrMsg := &common.Message{
					ReplicationResponse: &common.ReplicationResponse{
						Data: []common.QuorumRound{{Block: b5, Finalization: &f5}},
					},
				}

				threshold := common.F(len(joinedSet)) + 1
				msgs := make([]*messageInfo, 0, threshold+8)
				for i := 0; i < threshold; i++ {
					msgs = append(msgs, &messageInfo{msg: qrMsg, from: joinedSet.NodeIDs()[i]})
				}
				return tc, append(msgs,
					blockMsg(t, b3, testNodes),
					finalizationMsg(t, b3, testNodes),
					blockMsg(t, b4, joinedSet),
					finalizationMsg(t, b4, joinedSet),
					blockMsg(t, b5, otherSet),
					finalizationMsg(t, b5, otherSet),
					blockMsg(t, b6, joinedSet),
					finalizationMsg(t, b6, joinedSet),
				)
			},
			expectedCall: &transitionCall{epoch: 5, validators: joinedSet},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc, msgs := tt.setup(t)
			lastSeq := tc.seq

			var lock sync.Mutex
			calls := []transitionCall{}

			nv, err := NewNonValidator(
				Config{
					Storage:                    tc,
					Comm:                       testutil.NewNoopComm(tc.nodes().NodeIDs()),
					Logger:                     testutil.MakeLogger(t, 1),
					SignatureAggregatorCreator: tc.signatureAggregatorCreator,
					MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
					ID:                         newValidatorID,
					TransitionToValidator: func(epoch uint64, validators common.Nodes) {
						lock.Lock()
						defer lock.Unlock()
						calls = append(calls, transitionCall{epoch: epoch, validators: validators})
					},
					EpochsReplicated: true,
				},
			)
			require.NoError(t, err)
			defer nv.Stop()

			for _, m := range msgs {
				require.NoError(t, nv.HandleMessage(m.msg, m.from))
			}

			tc.WaitForBlockCommit(lastSeq)

			lock.Lock()
			defer lock.Unlock()

			if tt.expectedCall == nil {
				require.Empty(t, calls)
				return
			}
			require.Equal(t, []transitionCall{*tt.expectedCall}, calls)
		})
	}
}

// TestNonValidator_RequestHighestEpochOnStart verifies that a non-validator
// starting behind the network issues a replication request for the highest
// epoch on startup.
func TestNonValidator_RequestHighestEpochOnStart(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	tc.indexEpochs(4, 8)

	myNodeID := common.NodeID{100}
	comm := &routerComm{
		messageQueue: &messageQueue{},
		ID:           myNodeID,
		nodes:        tc.nodes(),
	}

	nvStorage := tc.CloneUntil(2)
	nv, err := NewNonValidator(
		Config{
			Storage:                    nvStorage,
			Comm:                       comm,
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         myNodeID,
		},
	)

	require.NoError(t, err)

	nv.Start()
	defer nv.Stop()

	msg, ok := comm.messageQueue.popResponse()
	require.True(t, ok)
	require.NotNil(t, msg.msg.ReplicationRequest)
	require.Equal(t, uint64(1), msg.msg.ReplicationRequest.LatestFinalizedSeq)
	require.Empty(t, msg.msg.ReplicationRequest.Seqs)
}

// TestNonValidator_EpochReplication ensures a non-validator can replicate sequences given different states of the chain.
func TestNonValidator_EpochReplication(t *testing.T) {
	tests := []struct {
		name string
		// setup builds the full network chain the non-validator replicates from.
		setup             func(t *testing.T) *testChain
		maxSequenceWindow uint64
		// initialHeight is the number of blocks the non-validator's storage is
		// seeded with before replication begins.
		initialHeight uint64
		lastSeq       uint64
	}{
		{
			// Replicates multiple epochs within a single replication window.
			name: "replicates epochs",
			setup: func(t *testing.T) *testChain {
				tc := newSeededChain(t, testNodes, 2)
				tc.indexEpochs(5, 10, 20, 30, 40)
				return tc
			},
			maxSequenceWindow: 50,
			initialHeight:     2,
			lastSeq:           40,
		},
		{
			// A small window forces replication well past the max round window.
			name: "past max round window",
			setup: func(t *testing.T) *testChain {
				tc := newSeededChain(t, testNodes, 2)
				tc.indexEpochs(5, 10, 20, 30, 40, 50, 60, 80, 100)
				return tc
			},
			maxSequenceWindow: 5, // significantly lower
			initialHeight:     2,
			lastSeq:           100,
		},
		{
			// Storage starts with only the genesis block.
			name: "from genesis",
			setup: func(t *testing.T) *testChain {
				tc := newSeededChain(t, testNodes, 2)
				tc.indexEpochs(5, 10, 20)
				return tc
			},
			maxSequenceWindow: 50,
			initialHeight:     1, // genesis is the only block
			lastSeq:           20,
		},
		{
			// Replicates a chain converted from snowman to simplex.
			name: "converted simplex chain",
			setup: func(t *testing.T) *testChain {
				tc := newSnowToSimplexChain(t, 10)
				firstBlock := tc.appendFirstSimplexAfterGenesis(testNodes)
				require.NoError(t, tc.Index(context.Background(), firstBlock, tc.newFinalization(firstBlock)))
				return tc
			},
			maxSequenceWindow: 50,
			initialHeight:     11, // lastSnowmanSeq + 1
			lastSeq:           11,
		},
		{
			// Converted chain that then spans several simplex epochs.
			name: "converted simplex chain many epochs",
			setup: func(t *testing.T) *testChain {
				tc := newSnowToSimplexChain(t, 10)
				firstBlock := tc.appendFirstSimplexAfterGenesis(testNodes)
				require.NoError(t, tc.Index(context.Background(), firstBlock, tc.newFinalization(firstBlock)))
				tc.indexEpochs(20, 30)
				return tc
			},
			maxSequenceWindow: 50,
			initialHeight:     11, // lastSnowmanSeq + 1
			lastSeq:           30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := tt.setup(t)
			myNodeID := common.NodeID{16}
			msgQueue := &messageQueue{}
			nonValidatorComm := &routerComm{
				nodes:        tc.nodes(),
				t:            tc.t,
				ID:           myNodeID,
				messageQueue: msgQueue,
			}
			epochs := newTestEpochs(tc, msgQueue, tt.maxSequenceWindow)
			epochs.start()
			defer epochs.stop()

			nvStorage := tc.CloneUntil(tt.initialHeight)
			require.Equal(t, tt.initialHeight, nvStorage.NumBlocks())

			nv, err := NewNonValidator(
				Config{
					Storage:                    nvStorage,
					Comm:                       nonValidatorComm,
					Logger:                     testutil.MakeLogger(t, 1),
					SignatureAggregatorCreator: tc.signatureAggregatorCreator,
					MaxSequenceWindow:          tt.maxSequenceWindow,
					ID:                         myNodeID,
					StartTime:                  time.Now(),
					EpochsReplicated:           false,
				},
			)
			require.NoError(t, err)

			nv.Start()
			defer nv.Stop()

			advanceUntil(nv, epochs, msgQueue, tt.lastSeq)
			require.Eventually(t, nv.HasReplicatedEpochs, 5*time.Second, 10*time.Millisecond)
		})
	}
}

func TestNonValidator_ReplicationRequests(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	lastSeq := uint64(40)
	initialHeight := uint64(2)
	tc.indexEpochs(5, 10, 20, 30, lastSeq)
	maxSeqWindow := uint64(50)
	myNodeID := common.NodeID{255}
	msgQueue := &messageQueue{}
	nonValidatorComm := &routerComm{
		nodes:        tc.nodes(),
		t:            tc.t,
		ID:           myNodeID,
		messageQueue: msgQueue,
	}
	epochs := newTestEpochs(tc, msgQueue, maxSeqWindow)
	epochs.start()
	defer epochs.stop()

	nvStorage := tc.CloneUntil(initialHeight)
	startTime := time.Now()
	nv, err := NewNonValidator(
		Config{
			Storage:                    nvStorage,
			Comm:                       nonValidatorComm,
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          maxSeqWindow,
			ID:                         myNodeID,
			StartTime:                  startTime,
			EpochsReplicated:           true,
		},
	)
	require.NoError(t, err)

	nv.Start()
	defer nv.Stop()

	count := 0
	for {
		for msg, ok := msgQueue.popResponse(); ok; {
			// drops 25% of messages
			if count%4 != 0 {
				handleMessage(epochs, nv, msg)
			}
			count++
			msg, ok = msgQueue.popResponse()
		}

		// check if storage has indexed all
		if lastSeq == nv.Storage.NumBlocks()-1 {
			break
		}

		// update the time so pending replication requests time out and re-fire
		startTime = startTime.Add(simplex.DefaultReplicationRequestTimeout)
		time.Sleep(50 * time.Millisecond)
		nv.AdvanceTime(startTime)
	}

	// clear in flight messages
	startTime = startTime.Add(simplex.DefaultReplicationRequestTimeout)
	nv.AdvanceTime(startTime)
	time.Sleep(50 * time.Millisecond)
	msgQueue.clearResponses()

	// ensure all timeout tasks were removed: advancing time should no longer
	// cause the non-validator to emit any further requests.
	count = 0
	for {
		startTime = startTime.Add(simplex.DefaultReplicationRequestTimeout)
		nv.AdvanceTime(startTime)
		time.Sleep(50 * time.Millisecond)

		msg, ok := msgQueue.popResponse()
		require.False(t, ok, fmt.Sprintf("all replication request tasks should be finished %v", msg))

		if count > 3 {
			break
		}
		count++
	}
}

// We optimistically store quorum rounds for sequences that validate an epoch(even though we have not verified the finalization)
// This test ensures, we verify the finalization when it is time to process it. If the finalization is incorrect, we need to re-request
// from the replicator.
func TestNonValidator_VerifiesFinalizationDuringReplication(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	epoch3Nodes := append(testNodes, common.Node{
		Id:     common.NodeID{byte(10)},
		Weight: 1,
	})
	epoch4Nodes := append(epoch3Nodes, common.Node{
		Id:     common.NodeID{byte(11)},
		Weight: 1,
	})

	startTime := time.Now()
	storage := testutil.NewInMemStorage()
	require.NoError(t, storage.Index(context.Background(), genesis, common.Finalization{}))

	nv, err := NewNonValidator(
		Config{
			Storage:                    storage,
			Comm:                       testutil.NewNoopComm(epoch4Nodes.NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          5, // significantly lower the max round window
			ID:                         testNodes.NodeIDs()[0],
			StartTime:                  startTime,
			EpochsReplicated:           true,
		},
	)

	require.NoError(t, err)
	nv.Start()
	defer nv.Stop()

	s3, s4 := tc.appendSealing(epoch3Nodes), tc.appendSealing(epoch4Nodes)
	f3 := tc.newFinalization(s3)
	brokenFinalization := tc.newFinalization(s4)
	brokenFinalization.Finalization.Epoch = 9000
	brokenFinalization.QC = errQC{QuorumCertificate: brokenFinalization.QC}

	brokenQR := common.QuorumRound{
		Block:        s4,
		Finalization: &brokenFinalization,
	}
	brokenReplicationResponse := &common.ReplicationResponse{
		LatestSeq: &brokenQR,
	}

	// With 6 nodes, the threshold votes for validating the highest epoch is  2. Because F + 1 = 2.
	require.NoError(t, nv.HandleMessage(
		&common.Message{
			ReplicationResponse: brokenReplicationResponse,
		},
		testNodes.NodeIDs()[2],
	))
	require.NoError(t, nv.HandleMessage(
		&common.Message{
			ReplicationResponse: brokenReplicationResponse,
		},
		testNodes.NodeIDs()[3],
	))

	// send seq 1, 2, 3
	s1, f1, err := tc.Retrieve(1)
	require.NoError(t, err)
	s2, f2, err := tc.Retrieve(2)
	require.NoError(t, err)

	require.NoError(t, nv.HandleMessage(
		&common.Message{
			ReplicationResponse: &common.ReplicationResponse{
				Data: []common.QuorumRound{
					{
						Block:        s3,
						Finalization: &f3,
					},
					{
						Block:        s1.(common.Block),
						Finalization: &f1,
					},
					{
						Block:        s2.(common.Block),
						Finalization: &f2,
					},
				},
			},
		},
		testNodes.NodeIDs()[3],
	))

	require.Never(t,
		func() bool {
			return storage.NumBlocks() >= 5
		},
		2*time.Second,
		50*time.Millisecond,
	)
}

func TestNonValidatorRejectsQuorumRoundFromNonValidator(t *testing.T) {
	validators := testNodes
	sigAggCreator := func(nodes []common.Node) common.SignatureAggregator {
		return &testutil.TestSignatureAggregator{N: len(nodes)}
	}

	// The attacker-controlled validator set the attacker wants us to adopt.
	// It is completely disjoint from the real validator set, so a block
	// finalized by it must never be committed by a correct non-validator.
	attackerSet := common.Nodes{
		{Id: common.NodeID{200}, Weight: 1},
		{Id: common.NodeID{201}, Weight: 1},
		{Id: common.NodeID{202}, Weight: 1},
	}

	makeZeroBlockQR := func(set common.Nodes) common.QuorumRound {
		block := newSealingTestBlock(1, 1, genesis.Digest, &common.SealingBlockInfo{
			ValidatorSet:         set,
			PrevSealingBlockHash: genesis.Digest,
		})
		fin, _ := testutil.NewFinalizationRecord(t, sigAggCreator(set), block, set.NodeIDs())
		return common.QuorumRound{Block: block, Finalization: &fin}
	}

	threshold := common.F(len(validators)) + 1

	tests := []struct {
		name string
		// epochSet is the validator set embedded in the first-epoch sealing block that the non-validator receives.
		// It is either a forged set or the real set.
		epochSet common.Nodes
		// senders are the peers that vouch for the sealing block.
		senders []common.NodeID
		// expectCommit is whether the non-validator should commit the block.
		expectCommit bool
	}{
		{
			name:         "forged epoch vouched for by non-validators",
			epochSet:     attackerSet,
			senders:      []common.NodeID{{100}, {101}, {102}},
			expectCommit: false,
		},
		{
			name:         "authentic epoch vouched for by validators",
			epochSet:     validators,
			senders:      validators.NodeIDs(),
			expectCommit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(tt.senders), threshold)

			storage := testutil.NewInMemStorage()
			require.NoError(t, storage.Index(context.Background(), genesis, common.Finalization{}))

			nv, err := NewNonValidator(
				Config{
					Storage:                    storage,
					Comm:                       testutil.NewNoopComm(validators.NodeIDs()),
					Logger:                     testutil.MakeLogger(t, 1),
					SignatureAggregatorCreator: sigAggCreator,
					MaxSequenceWindow:          10,
					ID:                         validators.NodeIDs()[0],
					StartTime:                  time.Now(),
					EpochsReplicated:           true,
				},
			)
			require.NoError(t, err)
			nv.Start()
			defer nv.Stop()

			qr := makeZeroBlockQR(tt.epochSet)

			// Deliver f+1 quorum rounds, each from a distinct sender, all vouching
			// for the same sealing block.
			for i := 0; i < threshold; i++ {
				require.NoError(t, nv.HandleMessage(&common.Message{
					ReplicationResponse: &common.ReplicationResponse{
						Data: []common.QuorumRound{qr},
					},
				}, tt.senders[i]))
			}

			if tt.expectCommit {
				committed := storage.WaitForBlockCommit(1)
				require.Equal(t, qr.Block.BlockHeader().Digest, committed.BlockHeader().Digest)
				require.Equal(t, uint64(2), storage.NumBlocks())
				return
			}

			require.Never(t,
				func() bool { return storage.NumBlocks() > 1 },
				2*time.Second, 50*time.Millisecond,
				"non-validator committed a block finalized by a validator set that only non-validators vouched for (C2 fork)",
			)
		})
	}
}

// TestNonValidator_EpochReplicationGatesMessages asserts that blocks and finalizations are dropped
// until a threshold of replication responses vote for the same sealing block, after which
// the stored round is committed and messages are processed normally.
func TestNonValidator_EpochReplicationGatesMessages(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)

	nv, err := NewNonValidator(
		Config{
			Storage:                    tc,
			Comm:                       testutil.NewNoopComm(testNodes.NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         common.NodeID{100},
		},
	)
	require.NoError(t, err)
	defer nv.Stop()

	b3 := tc.appendSealing(testNodes)
	f3 := tc.newFinalization(b3)

	block := blockMsg(t, b3, testNodes)
	require.NoError(t, nv.HandleMessage(block.msg, block.from))
	fin := finalizationMsg(t, b3, testNodes)
	require.NoError(t, nv.HandleMessage(fin.msg, fin.from))

	require.Never(t,
		func() bool { return tc.NumBlocks() > 3 },
		2*time.Second, 50*time.Millisecond,
		"indexed a block before epochs were replicated",
	)

	qrMsg := &common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			Data: []common.QuorumRound{{Block: b3, Finalization: &f3}},
		},
	}
	threshold := common.F(len(testNodes)) + 1
	for i := 0; i < threshold-1; i++ {
		require.NoError(t, nv.HandleMessage(qrMsg, testNodes.NodeIDs()[i]))
	}
	require.False(t, nv.HasReplicatedEpochs(), "epochsReplicated below the threshold")

	// the sealing block's epoch was opened by the indexed epoch 1, so the threshold vote finishes epoch replication
	require.NoError(t, nv.HandleMessage(qrMsg, testNodes.NodeIDs()[threshold-1]))
	require.True(t, nv.HasReplicatedEpochs())
	tc.WaitForBlockCommit(3)

	// messages flow normally after epoch replication
	b4 := tc.appendBlock()
	block = blockMsg(t, b4, testNodes)
	require.NoError(t, nv.HandleMessage(block.msg, block.from))
	fin = finalizationMsg(t, b4, testNodes)
	require.NoError(t, nv.HandleMessage(fin.msg, fin.from))
	tc.WaitForBlockCommit(4)
}

// sealingResponse wraps the block and finalization indexed at seq on tc in a replication response.
func sealingResponse(tc *testChain, seq uint64) *common.Message {
	block, fin, err := tc.Retrieve(seq)
	require.NoError(tc.t, err)
	return &common.Message{ReplicationResponse: &common.ReplicationResponse{
		Data: []common.QuorumRound{{Block: block.(common.Block), Finalization: &fin}},
	}}
}

// popRequestedSeqs drains the message queue and returns every seq requested.
func popRequestedSeqs(t *testing.T, msgQueue *messageQueue) []uint64 {
	seqs := []uint64{}
	for msg, ok := msgQueue.popResponse(); ok; msg, ok = msgQueue.popResponse() {
		require.NotNil(t, msg.msg.ReplicationRequest, "unexpected message %v", msg.msg)
		seqs = append(seqs, msg.msg.ReplicationRequest.Seqs...)
	}
	return seqs
}

// TestNonValidator_EpochReplicationWalksHashChain asserts that once a threshold of nodes reports the latest
// sealing block, a non-validator requests each earlier sealing block that it points back to, and finishes
// validator set replication when it reaches an epoch it has already indexed.
func TestNonValidator_EpochReplicationWalksHashChain(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	tc.indexEpochs(5, 10, 20)
	myNodeID := common.NodeID{100}
	msgQueue := &messageQueue{}
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc.CloneUntil(3),
			Comm:                       &routerComm{nodes: tc.nodes(), t: t, ID: myNodeID, messageQueue: msgQueue},
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         myNodeID,
			StartTime:                  time.Now(),
		},
	)
	require.NoError(t, err)
	defer nv.Stop()

	threshold := common.F(len(tc.nodes())) + 1
	for i := 0; i < threshold; i++ {
		require.NoError(t, nv.HandleMessage(sealingResponse(tc, 20), tc.nodes().NodeIDs()[i]))
	}
	require.False(t, nv.HasReplicatedEpochs())

	// each sealing block validates by hash and requests the one that opened its epoch
	for _, seq := range []uint64{10, 5} {
		require.Equal(t, []uint64{seq}, popRequestedSeqs(t, msgQueue))
		require.False(t, nv.HasReplicatedEpochs())
		require.NoError(t, nv.HandleMessage(sealingResponse(tc, seq), tc.nodes().NodeIDs()[0]))
	}

	// seq 5 was opened by the indexed epoch 1, so every sealing block is validated
	require.True(t, nv.HasReplicatedEpochs())
	require.NotEmpty(t, popRequestedSeqs(t, msgQueue), "replication of the sequences behind the tip never started")
}

// TestNonValidator_EpochReplicationIgnoresSealingBlockOffChain asserts that while following the hash chain
// a sealing block further down is not processed until the epoch pointing back to it has been validated.
func TestNonValidator_EpochReplicationIgnoresSealingBlockOffChain(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	tc.indexEpochs(5, 10, 20)
	myNodeID := common.NodeID{100}
	msgQueue := &messageQueue{}
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc.CloneUntil(3),
			Comm:                       &routerComm{nodes: tc.nodes(), t: t, ID: myNodeID, messageQueue: msgQueue},
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         myNodeID,
			StartTime:                  time.Now(),
		},
	)
	require.NoError(t, err)
	defer nv.Stop()

	threshold := common.F(len(tc.nodes())) + 1
	for i := 0; i < threshold; i++ {
		require.NoError(t, nv.HandleMessage(sealingResponse(tc, 20), tc.nodes().NodeIDs()[i]))
	}
	require.Equal(t, []uint64{10}, popRequestedSeqs(t, msgQueue))

	// seq 5 opened the epoch seq 10 was produced in, but seq 10 has not been validated yet
	require.NoError(t, nv.HandleMessage(sealingResponse(tc, 5), tc.nodes().NodeIDs()[0]))
	require.Empty(t, popRequestedSeqs(t, msgQueue))
	require.False(t, nv.HasReplicatedEpochs())

	// once seq 10 validates, seq 5 is still requested, meaning it was not processed before, and is accepted on resend
	require.NoError(t, nv.HandleMessage(sealingResponse(tc, 10), tc.nodes().NodeIDs()[0]))
	require.Equal(t, []uint64{5}, popRequestedSeqs(t, msgQueue))
	require.NoError(t, nv.HandleMessage(sealingResponse(tc, 5), tc.nodes().NodeIDs()[0]))
	require.True(t, nv.HasReplicatedEpochs())
}

// TestNonValidator_EpochReplicationRetriesSealingBlock asserts an unanswered request for a sealing block
// of the hash chain is re-requested once the replication timeout passes.
func TestNonValidator_EpochReplicationRetriesSealingBlock(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	tc.indexEpochs(5, 10, 20)
	myNodeID := common.NodeID{100}
	msgQueue := &messageQueue{}
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc.CloneUntil(3),
			Comm:                       &routerComm{nodes: tc.nodes(), t: t, ID: myNodeID, messageQueue: msgQueue},
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         myNodeID,
			StartTime:                  time.Now(),
		},
	)
	require.NoError(t, err)
	defer nv.Stop()

	threshold := common.F(len(tc.nodes())) + 1
	for i := 0; i < threshold; i++ {
		require.NoError(t, nv.HandleMessage(sealingResponse(tc, 20), tc.nodes().NodeIDs()[i]))
	}
	require.Equal(t, []uint64{10}, popRequestedSeqs(t, msgQueue))

	nv.AdvanceTime(nv.StartTime.Add(simplex.DefaultReplicationRequestTimeout))
	require.Eventually(t, func() bool {
		msg, ok := msgQueue.popResponse()
		return ok && slices.Equal(msg.msg.ReplicationRequest.Seqs, []uint64{10})
	}, 5*time.Second, 10*time.Millisecond, "the sealing block was never re-requested")
}

// TestNonValidator_EpochReplicationRequestsSealingBlock asserts that a replication response carrying a
// non-sealing block while replicating epochs, triggers a request for the sealing block that
// opened its epoch.
func TestNonValidator_EpochReplicationRequestsSealingBlock(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	myNodeID := common.NodeID{100}
	msgQueue := &messageQueue{}
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc.CloneUntil(3),
			Comm:                       &routerComm{nodes: testNodes, t: t, ID: myNodeID, messageQueue: msgQueue},
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         myNodeID,
		},
	)
	require.NoError(t, err)
	defer nv.Stop()

	// seq 3, which is still in the first epoch.
	epoch1Block := tc.appendBlock()
	require.NoError(t, tc.Index(context.Background(), epoch1Block, tc.newFinalization(epoch1Block)))
	// seq 4, the last block of epoch 1 and creates epoch 4
	epoch4SealingBlock := tc.appendSealing(testNodes)
	require.NoError(t, tc.Index(context.Background(), epoch4SealingBlock, tc.newFinalization(epoch4SealingBlock)))
	// the validator does not know about epoch 4
	blockInEpoch4 := tc.appendBlock()
	require.NoError(t, tc.Index(context.Background(), blockInEpoch4, tc.newFinalization(blockInEpoch4)))
	sender := testNodes.NodeIDs()[2]

	for _, tt := range []struct {
		receivedSeq        uint64 // The sequence the non-validator receives
		expectedRequestSeq uint64 // The expected sequence the non-validator requests after receiving
	}{
		{receivedSeq: epoch1Block.BlockHeader().Seq, expectedRequestSeq: 1},
		{receivedSeq: blockInEpoch4.BlockHeader().Seq, expectedRequestSeq: epoch4SealingBlock.BlockHeader().Seq},
	} {
		require.NoError(t, nv.HandleMessage(sealingResponse(tc, tt.receivedSeq), sender))
		msg, ok := msgQueue.popResponse()
		require.True(t, ok)
		require.NotNil(t, msg.msg.ReplicationRequest)
		require.Equal(t, []uint64{tt.expectedRequestSeq}, msg.msg.ReplicationRequest.Seqs)
		require.Equal(t, sender, msg.to)
	}

	// non-sealing blocks never complete epoch replication
	require.False(t, nv.EpochsReplicated)
	_, ok := msgQueue.popResponse()
	require.False(t, ok)
}

// TestNonValidator_EpochReplicationLatestKnownEpoch asserts a node caught up to the network
// replicates epochs from responses vouching for the sealing block of the latest epoch it
// already has indexed, without re-indexing it.
func TestNonValidator_EpochReplicationLatestKnownEpoch(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc,
			Comm:                       testutil.NewNoopComm(testNodes.NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         common.NodeID{100},
		},
	)
	require.NoError(t, err)
	require.False(t, nv.EpochsReplicated)
	defer nv.Stop()

	// the sealing block of epoch 1, indexed at seq 1
	sealing, fin, err := tc.Retrieve(1)
	require.NoError(t, err)

	qrMsg := &common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			LatestSeq: &common.QuorumRound{Block: sealing.(common.Block), Finalization: &fin},
		},
	}

	threshold := common.F(len(testNodes)) + 1
	for i := 0; i < threshold; i++ {
		require.NoError(t, nv.HandleMessage(qrMsg, testNodes.NodeIDs()[i]))
	}

	require.True(t, nv.EpochsReplicated)
	require.Equal(t, uint64(3), tc.NumBlocks())

	// messages flow normally after epoch replication
	b3 := tc.appendBlock()
	block := blockMsg(t, b3, testNodes)
	require.NoError(t, nv.HandleMessage(block.msg, block.from))
	fin3 := finalizationMsg(t, b3, testNodes)
	require.NoError(t, nv.HandleMessage(fin3.msg, fin3.from))
	tc.WaitForBlockCommit(3)
}

// TestNonValidator_EpochThenSequenceReplication ensures a non validator replicates sealing blocks first,
// followed by blocks in between. Epochs replicate backwards from the tip first, then sequences within the epochs are replicated.
func TestNonValidator_EpochThenSequenceReplication(t *testing.T) {
	tc := newSeededChain(t, testNodes, 1)
	tc.indexEpochs(5, 10)
	blockSeq11 := tc.appendBlock()
	require.NoError(t, tc.Index(context.Background(), blockSeq11, tc.newFinalization(blockSeq11)))

	myNodeID := common.NodeID{100}
	msgQueue := &messageQueue{}
	storage := tc.CloneUntil(2)
	nv, err := NewNonValidator(
		Config{
			Storage:                    storage,
			Comm:                       &routerComm{nodes: tc.nodes(), t: t, ID: myNodeID, messageQueue: msgQueue},
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         myNodeID,
			StartTime:                  time.Now(),
		},
	)
	require.NoError(t, err)
	defer nv.Stop()

	validators := tc.nodes().NodeIDs()
	threshold := common.F(len(validators)) + 1

	// a threshold of nodes send sealing block 10
	for _, id := range validators[:threshold] {
		require.NoError(t, nv.HandleMessage(sealingResponse(tc, 10), id))
	}
	// ensure we request sequence 5 first (it is the next sealing block in the backwards hash chain).
	require.Equal(t, []uint64{5}, popRequestedSeqs(t, msgQueue))

	// seq 5 points back to the indexed seq 1, finishing epoch replication
	require.NoError(t, nv.HandleMessage(sealingResponse(tc, 5), validators[0]))
	require.True(t, nv.HasReplicatedEpochs())
	require.Equal(t, []uint64{2, 3, 4, 5, 6, 7, 8, 9, 10}, popRequestedSeqs(t, msgQueue))

	// the sealing blocks are already stored, so only the rest are sent
	for _, seq := range []uint64{2, 3, 4, 6, 7, 8, 9, 11} {
		require.NoError(t, nv.HandleMessage(sealingResponse(tc, seq), validators[0]))
	}
	storage.WaitForBlockCommit(11)
}

func advanceUntil(nv *NonValidator, epochs *testEpochs, msgQueue *messageQueue, seq uint64) {
	startTime := nv.StartTime
	for {
		// Send any requests as responses back to the node
		for msg, ok := msgQueue.popResponse(); ok; {
			handleMessage(epochs, nv, msg)
			msg, ok = msgQueue.popResponse()
		}

		// check if storage has indexed all
		if seq == nv.Storage.NumBlocks()-1 {
			break
		}

		// update the time
		startTime = startTime.Add(simplex.DefaultReplicationRequestTimeout)
		time.Sleep(50 * time.Millisecond)
		nv.AdvanceTime(startTime)
	}
}

func TestNilBlockPanicMissingBlock(t *testing.T) {
	startTime := time.Now()
	storage := testutil.NewInMemStorage()
	require.NoError(t, storage.Index(context.Background(), genesis, common.Finalization{}))

	tc := newSeededChain(t, testNodes, 2)

	nv, err := NewNonValidator(
		Config{
			Storage:                    storage,
			Comm:                       testutil.NewNoopComm(testNodes.NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          5,
			ID:                         testNodes.NodeIDs()[0],
			StartTime:                  startTime,
		},
	)
	require.NoError(t, err)
	nv.Start()
	defer nv.Stop()

	malicious := common.QuorumRound{
		Block:             nil,
		EmptyNotarization: &common.EmptyNotarization{},
		Finalization:      &common.Finalization{},
	}

	resp := &common.ReplicationResponse{
		Data: []common.QuorumRound{malicious},
	}

	require.NoError(t, nv.HandleMessage(&common.Message{ReplicationResponse: resp}, testNodes.NodeIDs()[2]))
}

// TestNonValidatorAcceptsProposalFromUnsortedValidatorSet asserts that a non-validator elects the
// same leader as the validators when the validator set it was handed is not in sorted order.
//
// simplex.LeaderForRound indexes with round % len(nodes), so it is order sensitive. Validators
// sort their set in Epoch.init, so if the non-validator keeps the set in the order it arrived, the
// two disagree about who leads every round. handleBlock then drops every live proposal as "not
// from the leader of that round" and the non-validator silently degrades to replication only.
func TestNonValidatorAcceptsProposalFromUnsortedValidatorSet(t *testing.T) {
	unsortedNodes := common.Nodes{
		{Id: common.NodeID{4}, Weight: 1},
		{Id: common.NodeID{2}, Weight: 1},
		{Id: common.NodeID{3}, Weight: 1},
		{Id: common.NodeID{1}, Weight: 1},
	}

	// The order the validators run with, and therefore the leader the network actually elects.
	sortedNodes := slices.Clone(unsortedNodes)
	common.SortNodes(sortedNodes)

	// The sealing block carries the set unsorted.
	tc := newSeededChain(t, unsortedNodes, 2)
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc,
			Comm:                       testutil.NewNoopComm(sortedNodes.NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         common.NodeID{100},
			EpochsReplicated:           true,
		},
	)
	require.NoError(t, err)

	nv.Start()
	defer nv.Stop()

	b := tc.appendBlock()
	require.NotEqual(t,
		simplex.LeaderForRound(unsortedNodes.NodeIDs(), b.BlockHeader().Round),
		simplex.LeaderForRound(sortedNodes.NodeIDs(), b.BlockHeader().Round),
		"test requires a round whose leader differs between the two orderings")

	// The proposal arrives from the leader the validators elected.
	block := blockMsg(t, b, sortedNodes)
	require.NoError(t, nv.HandleMessage(block.msg, block.from))
	fin := finalizationMsg(t, b, sortedNodes)
	require.NoError(t, nv.HandleMessage(fin.msg, fin.from))

	require.Eventually(t, func() bool {
		return tc.NumBlocks() == b.BlockHeader().Seq+1
	}, 5*time.Second, 10*time.Millisecond,
		"non-validator never committed the block, having dropped the proposal because it ordered the validator set differently than the validators")
}

func TestNonValidatorRejectsQuorumRoundWithMismatchedHeader(t *testing.T) {
	tc := newSeededChain(t, testNodes, 4)

	storage := tc.CloneUntil(3)
	logger := testutil.MakeLogger(t, 1)

	nv, err := NewNonValidator(
		Config{
			Storage:                    storage,
			Comm:                       testutil.NewNoopComm(testNodes.NodeIDs()),
			Logger:                     logger,
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			EpochsReplicated:           true,
			MaxSequenceWindow:          10,
			ID:                         common.NodeID{16},
			StartTime:                  time.Now(),
		},
	)
	require.NoError(t, err)
	nv.Start()
	defer nv.Stop()

	b4, finalization, err := tc.Retrieve(3)
	require.NoError(t, err)
	b3 := b4.(common.Block)

	// Same digest and seq as the b4, but a round the b4 was not finalized in.
	mismatched := finalization
	mismatched.Finalization.Round++
	require.NoError(t, nv.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			Data: []common.QuorumRound{{Block: b3, Finalization: &mismatched}},
		},
	}, testNodes.NodeIDs()[0]))

	require.NoError(t, nv.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			Data: []common.QuorumRound{{Block: b3, Finalization: &finalization}},
		},
	}, testNodes.NodeIDs()[1]))

	storage.WaitForBlockCommit(3)
	_, finalization, err = storage.Retrieve(3)
	require.NoError(t, err)
	bh := b3.BlockHeader()
	require.True(t, finalization.Finalization.Equals(&bh), "b4 was indexed with a finalization that does not match its header")
}

// TestNonValidatorDropsQuorumRoundPastSequenceWindow ensures non-validators dont store quorum rounds past MaxSequenceWindow.
// Asserts the non-validator commits the sequences within the window and then stalls on the one past it, rather than
// indexing a block it was streamed while too far behind.
func TestNonValidatorDropsQuorumRoundPastSequenceWindow(t *testing.T) {
	maxSequenceWindow := uint64(5)
	initialHeight := uint64(2)
	maxSequenceToStore := initialHeight + maxSequenceWindow
	maxSequenceToSend := maxSequenceToStore + 2

	tc := newSeededChain(t, testNodes, maxSequenceToSend)
	storage := tc.CloneUntil(initialHeight)
	require.Equal(t, initialHeight, storage.NumBlocks(), "the next sequence to commit sets where the window starts")

	nv, err := NewNonValidator(
		Config{
			Storage:                    storage,
			Comm:                       testutil.NewNoopComm(testNodes.NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          maxSequenceWindow,
			ID:                         common.NodeID{16},
			StartTime:                  time.Now(),
			EpochsReplicated:           true,
		},
	)
	require.NoError(t, err)
	nv.Start()
	defer nv.Stop()

	data := make([]common.QuorumRound, 0, maxSequenceToSend-initialHeight+1)
	for seq := initialHeight; seq <= maxSequenceToSend; seq++ {
		block, finalization, err := tc.Retrieve(seq)
		require.NoError(t, err)
		data = append(data, common.QuorumRound{Block: block.(common.Block), Finalization: &finalization})
	}

	require.NoError(t, nv.HandleMessage(&common.Message{
		ReplicationResponse: &common.ReplicationResponse{Data: data},
	}, testNodes.NodeIDs()[0]))

	// NumBlocks counts blocks, so committing maxSequenceToStore leaves maxSequenceToStore+1 of
	// them. Any more means a sequence past the window was stored and indexed.
	storage.WaitForBlockCommit(maxSequenceToStore)
	require.Never(t,
		func() bool { return storage.NumBlocks() > maxSequenceToStore+1 },
		time.Second, 50*time.Millisecond,
		"indexed a block that was past the sequence window when it was received",
	)
}

// TestNonValidatorIgnoresReplayedQuorumRoundWhileVerifying asserts that replaying the finalized
// quorum round for nextSeqToCommit while its verification task is in flight does not schedule
// duplicate tasks.
func TestNonValidatorIgnoresReplayedQuorumRoundWhileVerifying(t *testing.T) {
	tc := newSeededChain(t, testNodes, 2)
	storage := tc.CloneUntil(2)

	block, finalization, err := tc.Retrieve(2)
	require.NoError(t, err)

	// Hold the first verification so the replays arrive while its task is still running.
	release := make(chan struct{})
	var verifications atomic.Int32
	tb := block.(*testutil.TestBlock)
	tb.VerificationError = errors.New("verification failed")
	tb.OnVerify = func() {
		if verifications.Add(1) == 1 {
			<-release
		}
	}

	nv, err := NewNonValidator(
		Config{
			Storage:                    storage,
			Comm:                       testutil.NewNoopComm(testNodes.NodeIDs()),
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          5,
			ID:                         common.NodeID{16},
			StartTime:                  time.Now(),
		},
	)
	require.NoError(t, err)
	nv.Start()
	defer nv.Stop()

	// Send many of the same responses that
	for range 10 {
		require.NoError(t, nv.HandleMessage(
			&common.Message{ReplicationResponse: &common.ReplicationResponse{
				Data: []common.QuorumRound{{Block: tb, Finalization: &finalization}},
			}},
			common.NodeID{42},
		))
	}
	close(release)

	require.Never(t, func() bool { return verifications.Load() > 1 }, time.Second, 10*time.Millisecond,
		"a replayed quorum round scheduled a duplicate verification task")
}

// TestNonValidatorServesReplicationRequest asserts a non-validator answers a replication request from storage.
// Seqs it has not indexed are not sent and the latest finalized seq is its tip.
func TestNonValidatorServesReplicationRequest(t *testing.T) {
	tc := newSeededChain(t, testNodes, 5)
	msgQueue := &messageQueue{}
	nvID := common.NodeID{100}

	nv, err := NewNonValidator(
		Config{
			Storage:                    tc,
			Comm:                       &routerComm{t: t, nodes: testNodes, ID: nvID, messageQueue: msgQueue},
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         nvID,
		},
	)
	require.NoError(t, err)
	defer nv.Stop()

	requester := testNodes[0].Id
	req := &common.Message{ReplicationRequest: &common.ReplicationRequest{Seqs: []uint64{9, 3, 2}, LatestFinalizedSeq: 1}}
	require.NoError(t, nv.HandleMessage(req, requester))

	mi, ok := msgQueue.popResponse()
	require.True(t, ok)
	require.Equal(t, requester, mi.to)
	resp := mi.msg.ReplicationResponse
	require.NotNil(t, resp)

	require.Len(t, resp.Data, 2)
	for i, seq := range []uint64{2, 3} {
		block, fin, err := tc.Retrieve(seq)
		require.NoError(t, err)
		require.Equal(t, block, resp.Data[i].Block)
		require.Equal(t, fin, *resp.Data[i].Finalization)
	}
	require.Equal(t, uint64(5), resp.LatestSeq.Block.BlockHeader().Seq)

	// nothing to serve for a seq past our tip
	req = &common.Message{ReplicationRequest: &common.ReplicationRequest{Seqs: []uint64{6}, LatestFinalizedSeq: 6}}
	require.NoError(t, nv.HandleMessage(req, requester))
	_, ok = msgQueue.popResponse()
	require.False(t, ok)
}

// TestNonValidatorRoutesReplicationRequestsToValidators tests a scenario where a Byzantine validator reports the genuine
// latest sealing block with a forged finalization, whose QC lists a node that does not exist.
// The finalization cannot be verified yet, so the non-validator must request the missing blocks from the validators
// rather than from the signers of that QC.
func TestNonValidatorRoutesReplicationRequestsToValidators(t *testing.T) {
	// The chain has 10 blocks, with sealing blocks at seqs 5 and 10. The non-validator has only seqs 0 and 1.
	tc := newSeededChain(t, testNodes, 1)
	tc.indexEpochs(5, 10)

	myNodeID := common.NodeID{100}
	msgQueue := &messageQueue{}
	nv, err := NewNonValidator(
		Config{
			Storage:                    tc.CloneUntil(2),
			Comm:                       &routerComm{nodes: tc.nodes(), t: t, ID: myNodeID, messageQueue: msgQueue},
			Logger:                     testutil.MakeLogger(t, 1),
			SignatureAggregatorCreator: tc.signatureAggregatorCreator,
			MaxSequenceWindow:          simplex.DefaultMaxRoundWindow,
			ID:                         myNodeID,
			StartTime:                  time.Now(),
		},
	)
	require.NoError(t, err)
	defer nv.Stop()

	// With 4 validators, F+1 = 2 reports of the same sealing block are enough to accept it.
	validators := tc.nodes()
	require.Equal(t, 2, common.F(len(validators))+1)
	honestValidator := validators[0].Id
	byzantineValidator := validators[1].Id

	// The Byzantine validator builds its response from the genuine sealing block 10, but swaps the finalization's QC
	// for one signed only by a node that doesn't exist.
	nonExistentNode := common.NodeID{99}
	require.False(t, validators.Contains(nonExistentNode))

	sealingBlock10, genuineFinalization, err := tc.Retrieve(10)
	require.NoError(t, err)
	forgedFinalization := common.Finalization{
		Finalization: genuineFinalization.Finalization, // the header matches the block, so it passes the header check
		QC:           testutil.TestQC{{Signer: nonExistentNode, Value: []byte("forged")}},
	}
	forgedResponse := &common.Message{ReplicationResponse: &common.ReplicationResponse{
		Data: []common.QuorumRound{{Block: sealingBlock10.(common.Block), Finalization: &forgedFinalization}},
	}}

	// The honest validator reports sealing block 10 first. The Byzantine validator's report is the second one, which reaches
	// the threshold, so its forged finalization is the one stored with the sealing block.
	require.NoError(t, nv.HandleMessage(sealingResponse(tc, 10), honestValidator))
	require.NoError(t, nv.HandleMessage(forgedResponse, byzantineValidator))

	// The non-validator walks the hash chain back from sealing block 10 and requests the previous sealing block, seq 5.
	require.Equal(t, []uint64{5}, popRequestedSeqs(t, msgQueue))

	// Sealing block 5 points back to an epoch the non-validator already has, so it now knows every validator set.
	require.NoError(t, nv.HandleMessage(sealingResponse(tc, 5), honestValidator))
	require.True(t, nv.HasReplicatedEpochs())

	// Next it requests the missing blocks 2 to 10. Each request must go to a validator.
	// With the bug, every request goes to the non-existent node from the forged QC.
	var requested, requestedFromNonValidators []uint64
	for msg, ok := msgQueue.popResponse(); ok; msg, ok = msgQueue.popResponse() {
		require.NotNil(t, msg.msg.ReplicationRequest, "unexpected message %v", msg.msg)
		requested = append(requested, msg.msg.ReplicationRequest.Seqs...)
		if !validators.Contains(msg.to) {
			requestedFromNonValidators = append(requestedFromNonValidators, msg.msg.ReplicationRequest.Seqs...)
		}
	}
	require.NotEmpty(t, requested, "the non-validator never requested the missing blocks")
	require.Empty(t, requestedFromNonValidators, "missing blocks were requested from the forged QC's signer instead of a validator")
}
