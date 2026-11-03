// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nonvalidator

import (
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

const listenerEpoch = 10

var listenerValidators = common.Nodes{
	{Id: common.NodeID{1}, Weight: 1},
	{Id: common.NodeID{2}, Weight: 1},
	{Id: common.NodeID{3}, Weight: 1},
	{Id: common.NodeID{4}, Weight: 1},
}

type sentMessage struct {
	msg *common.Message
	to  common.NodeID
}

type recordingSender struct {
	sent []sentMessage
}

func (r *recordingSender) Send(msg *common.Message, to common.NodeID) {
	r.sent = append(r.sent, sentMessage{msg: msg, to: to})
}

// finalizedQR returns a finalized quorum round for a block at seq in epoch, sealing when sealing is true.
func finalizedQR(seq, epoch uint64, sealing bool) common.QuorumRound {
	block := newBlock(seq, epoch, common.Digest{})
	if sealing {
		block.SealingInfo = &common.SealingBlockInfo{ValidatorSet: listenerValidators}
	}
	return common.QuorumRound{
		Block:        block,
		Finalization: &common.Finalization{Finalization: common.ToBeSignedFinalization{BlockHeader: block.BlockHeader()}},
	}
}

func replicationResponse(qrs ...common.QuorumRound) *common.Message {
	return &common.Message{ReplicationResponse: &common.ReplicationResponse{Data: qrs}}
}

type triggeredListener struct {
	*FutureEpochListener
	sender    *recordingSender
	triggered []*common.QuorumRound
}

func newTestFutureEpochListener(t *testing.T) *triggeredListener {
	tl := &triggeredListener{sender: &recordingSender{}}
	tl.FutureEpochListener = NewFutureEpochListener(testutil.MakeLogger(t, 1), tl.sender, listenerEpoch,
		func() common.Nodes { return listenerValidators },
		countSignatureAggregatorCreator,
		func(qr *common.QuorumRound) { tl.triggered = append(tl.triggered, qr) })
	return tl
}

// TestFutureEpochListenerRequestsSealingBlock asserts the listener requests the sealing block
// of the highest epoch a message references, from the sender, only when that epoch is after ours.
func TestFutureEpochListenerRequestsSealingBlock(t *testing.T) {
	from := listenerValidators[0].Id
	higherEpochBlock := newBlock(20, 15, common.Digest{})

	tests := []struct {
		name          string
		msg           *common.Message
		expectRequest []uint64
	}{
		{
			name: "finalization from our epoch",
			msg: &common.Message{Finalization: &common.Finalization{
				Finalization: common.ToBeSignedFinalization{BlockHeader: newBlock(20, listenerEpoch, common.Digest{}).BlockHeader()},
			}},
		},
		{
			name: "finalization from a higher epoch",
			msg: &common.Message{Finalization: &common.Finalization{
				Finalization: common.ToBeSignedFinalization{BlockHeader: higherEpochBlock.BlockHeader()},
			}},
			expectRequest: []uint64{15},
		},
		{
			name:          "block from a higher epoch",
			msg:           &common.Message{BlockMessage: &common.BlockMessage{Block: higherEpochBlock}},
			expectRequest: []uint64{15},
		},
		{
			name:          "replication response requests the highest epoch",
			msg:           replicationResponse(finalizedQR(20, 15, false), finalizedQR(30, 25, false), finalizedQR(12, listenerEpoch, false)),
			expectRequest: []uint64{25},
		},
		{
			name: "replication response without a finalization",
			msg:  replicationResponse(common.QuorumRound{Block: higherEpochBlock}),
		},
		{
			name: "replication response from our epoch",
			msg:  replicationResponse(finalizedQR(12, listenerEpoch, false)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestFutureEpochListener(t)
			l.HandleMessage(tt.msg, from)

			if tt.expectRequest == nil {
				require.Empty(t, l.sender.sent)
				return
			}
			require.Len(t, l.sender.sent, 1)
			require.Equal(t, from, l.sender.sent[0].to)
			require.Equal(t, tt.expectRequest, l.sender.sent[0].msg.ReplicationRequest.Seqs)
		})
	}
}

// TestFutureEpochListenerThreshold asserts onSealingBlock fires once per sealing block after f+1
// validators of the latest set report it, and never for a sealing block at or below our epoch.
func TestFutureEpochListenerThreshold(t *testing.T) {
	nonValidator := common.NodeID{9}

	tests := []struct {
		name string
		// responses[i] is sent by from[i]
		responses     []common.QuorumRound
		from          []common.NodeID
		expectTrigger []uint64
	}{
		{
			name:      "below threshold",
			responses: []common.QuorumRound{finalizedQR(20, listenerEpoch, true)},
			from:      []common.NodeID{listenerValidators[0].Id},
		},
		{
			name:      "same validator twice",
			responses: []common.QuorumRound{finalizedQR(20, listenerEpoch, true), finalizedQR(20, listenerEpoch, true)},
			from:      []common.NodeID{listenerValidators[0].Id, listenerValidators[0].Id},
		},
		{
			name: "a validator's higher sealing block replaces its lower one",
			responses: []common.QuorumRound{
				finalizedQR(20, listenerEpoch, true), finalizedQR(30, 20, true), finalizedQR(20, listenerEpoch, true),
			},
			from: []common.NodeID{listenerValidators[0].Id, listenerValidators[0].Id, listenerValidators[1].Id},
		},
		{
			name: "a validator's lower sealing block does not replace its higher one",
			responses: []common.QuorumRound{
				finalizedQR(30, 20, true), finalizedQR(20, listenerEpoch, true), finalizedQR(30, 20, true),
			},
			from:          []common.NodeID{listenerValidators[0].Id, listenerValidators[0].Id, listenerValidators[1].Id},
			expectTrigger: []uint64{30},
		},
		{
			name:      "non-validator does not count",
			responses: []common.QuorumRound{finalizedQR(20, listenerEpoch, true), finalizedQR(20, listenerEpoch, true)},
			from:      []common.NodeID{listenerValidators[0].Id, nonValidator},
		},
		{
			name:      "sealing block of our epoch",
			responses: []common.QuorumRound{finalizedQR(listenerEpoch, 5, true), finalizedQR(listenerEpoch, 5, true)},
			from:      []common.NodeID{listenerValidators[0].Id, listenerValidators[1].Id},
		},
		{
			name:          "threshold for the next epoch",
			responses:     []common.QuorumRound{finalizedQR(20, listenerEpoch, true), finalizedQR(20, listenerEpoch, true)},
			from:          []common.NodeID{listenerValidators[0].Id, listenerValidators[1].Id},
			expectTrigger: []uint64{20},
		},
		{
			name:          "threshold for an epoch several epochs ahead",
			responses:     []common.QuorumRound{finalizedQR(30, 20, true), finalizedQR(30, 20, true)},
			from:          []common.NodeID{listenerValidators[0].Id, listenerValidators[1].Id},
			expectTrigger: []uint64{30},
		},
		{
			name: "responses past the threshold do not trigger again",
			responses: []common.QuorumRound{
				finalizedQR(20, listenerEpoch, true), finalizedQR(20, listenerEpoch, true),
				finalizedQR(20, listenerEpoch, true), finalizedQR(20, listenerEpoch, true),
			},
			from:          []common.NodeID{listenerValidators[0].Id, listenerValidators[1].Id, listenerValidators[2].Id, listenerValidators[3].Id},
			expectTrigger: []uint64{20},
		},
		{
			name: "threshold for a lower epoch after a higher one is ignored",
			responses: []common.QuorumRound{
				finalizedQR(30, 20, true), finalizedQR(30, 20, true),
				finalizedQR(20, listenerEpoch, true), finalizedQR(20, listenerEpoch, true),
			},
			from:          []common.NodeID{listenerValidators[0].Id, listenerValidators[1].Id, listenerValidators[2].Id, listenerValidators[3].Id},
			expectTrigger: []uint64{30},
		},
		{
			name: "threshold for a lower epoch then a higher one",
			responses: []common.QuorumRound{
				finalizedQR(20, listenerEpoch, true), finalizedQR(20, listenerEpoch, true),
				finalizedQR(30, 20, true), finalizedQR(30, 20, true),
			},
			from:          []common.NodeID{listenerValidators[0].Id, listenerValidators[1].Id, listenerValidators[0].Id, listenerValidators[1].Id},
			expectTrigger: []uint64{20, 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestFutureEpochListener(t)
			for i, qr := range tt.responses {
				l.HandleMessage(replicationResponse(qr), tt.from[i])
			}

			var triggered []uint64
			for _, qr := range l.triggered {
				triggered = append(triggered, qr.Block.BlockHeader().Seq)
			}
			require.Equal(t, tt.expectTrigger, triggered)
		})
	}
}

// TestFutureEpochListenerMultipleResponses asserts a threshold can be reached partway through
// a response carrying many quorum rounds. Each validator's later sealing block replaces its earlier one,
// so only the highest sealing block reaches f+1 and triggers.
func TestFutureEpochListenerMultipleResponses(t *testing.T) {
	l := newTestFutureEpochListener(t)

	msg := replicationResponse(
		finalizedQR(15, 10, false),
		finalizedQR(20, 10, true), // sealing block for epoch 10
		finalizedQR(25, 20, false),
		finalizedQR(30, 20, true), // sealing block for epoch 20
		finalizedQR(35, 30, false),
	)

	l.HandleMessage(msg, listenerValidators[0].Id)
	require.Empty(t, l.triggered)

	l.HandleMessage(msg, listenerValidators[1].Id)
	require.Len(t, l.triggered, 1)

	// Only epoch 20's sealing block reaches threshold. Each validator's vote for epoch 10's
	// sealing block was replaced by the higher one later in the same response.
	require.Equal(t, uint64(30), l.triggered[0].Block.BlockHeader().Seq)
}
