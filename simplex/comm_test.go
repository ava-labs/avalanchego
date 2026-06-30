// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendermock"
	"github.com/ava-labs/avalanchego/utils/set"

	engcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

var testSimplexMessage = common.Message{
	VoteMessage: &common.Vote{
		Vote: common.ToBeSignedVote{
			BlockHeader: common.BlockHeader{
				ProtocolMetadata: common.ProtocolMetadata{
					Version: 1,
					Epoch:   1,
					Round:   1,
					Seq:     1,
				},
			},
		},
		Signature: common.Signature{
			Signer: []byte("dummy_node_id"),
			Value:  []byte("dummy_signature"),
		},
	},
}

func TestCommSendMessage(t *testing.T) {
	config := newEngineConfig(t, 1)

	destinationNodeID := ids.GenerateTestNodeID()

	comm, err := NewComm(config)
	require.NoError(t, err)

	outboundMsg, err := config.OutboundMsgBuilder.SimplexMessage(newVote(config.Ctx.ChainID, testSimplexMessage.VoteMessage))
	require.NoError(t, err)
	expectedSendConfig := engcommon.SendConfig{
		NodeIDs: set.Of(destinationNodeID),
	}
	sender := config.Sender.(*sendermock.ExternalSender)
	sender.EXPECT().Send(outboundMsg, expectedSendConfig, comm.subnetID, gomock.Any())

	comm.Send(&testSimplexMessage, destinationNodeID[:])
}

// TestCommBroadcast tests the Broadcast method sends to all nodes in the subnet
// not including the sending node.
func TestCommBroadcast(t *testing.T) {
	config := newEngineConfig(t, 3)
	sender := config.Sender.(*sendermock.ExternalSender)

	comm, err := NewComm(config)
	require.NoError(t, err)
	outboundMsg, err := config.OutboundMsgBuilder.SimplexMessage(newVote(config.Ctx.ChainID, testSimplexMessage.VoteMessage))
	require.NoError(t, err)
	nodes := make([]ids.NodeID, 0, len(comm.Validators()))
	for _, node := range comm.Validators() {
		if node.Id.Equals(config.Ctx.NodeID[:]) {
			continue // skip the sending node
		}
		nodes = append(nodes, ids.NodeID(node.Id))
	}

	expectedSendConfig := engcommon.SendConfig{
		NodeIDs: set.Of(nodes...),
	}

	sender.EXPECT().Send(outboundMsg, expectedSendConfig, comm.subnetID, gomock.Any())

	comm.Broadcast(&testSimplexMessage)
}

func TestCommFailsWithoutCurrentNode(t *testing.T) {
	config := newEngineConfig(t, 3)

	// set the curNode to a different nodeID than the one in the config
	vdrs := generateTestNodes(t, 3)
	config.Params = newSimplexChainParams(vdrs)

	_, err := NewComm(config)
	require.ErrorIs(t, err, errNodeNotFound)
}

func TestSimplexMessageReplicationResponse(t *testing.T) {
	chainID := ids.GenerateTestID()
	tests := []struct {
		name string
		resp *common.VerifiedReplicationResponse
	}{
		{
			name: "nil latest round",
			resp: &common.VerifiedReplicationResponse{
				Data: []common.VerifiedQuorumRound{
					{
						VerifiedBlock: &Block{
							metadata: common.ProtocolMetadata{},
							vmBlock:  snowmantest.Genesis,
						},
					},
				},
				LatestRound: nil,
			},
		},
		{
			name: "empty seqs",
			resp: &common.VerifiedReplicationResponse{
				Data:        []common.VerifiedQuorumRound{},
				LatestRound: nil,
			},
		},
		{
			name: "non-nil latest round",
			resp: &common.VerifiedReplicationResponse{
				Data: []common.VerifiedQuorumRound{},
				LatestRound: &common.VerifiedQuorumRound{
					VerifiedBlock: &Block{
						metadata: common.ProtocolMetadata{},
						vmBlock:  snowmantest.Genesis,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newReplicationResponse(chainID, tt.resp)
			require.NoError(t, err)
		})
	}
}
