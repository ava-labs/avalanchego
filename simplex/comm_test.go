// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendermock"
	"github.com/ava-labs/avalanchego/utils/set"

	simplexcommon "github.com/ava-labs/simplex/common"
)

var testSimplexMessage = simplexcommon.Message{
	VoteMessage: &simplexcommon.Vote{
		Vote: simplexcommon.ToBeSignedVote{
			BlockHeader: simplexcommon.BlockHeader{
				ProtocolMetadata: simplexcommon.ProtocolMetadata{
					Version: 1,
					Epoch:   1,
					Round:   1,
					Seq:     1,
				},
			},
		},
		Signature: simplexcommon.Signature{
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
	expectedSendConfig := common.SendConfig{
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
	nodes := make([]ids.NodeID, 0, len(comm.Validators().NodeIDs()))
	for _, node := range comm.Validators().NodeIDs() {
		if node.Equals(config.Ctx.NodeID[:]) {
			continue // skip the sending node
		}
		nodes = append(nodes, ids.NodeID(node))
	}

	expectedSendConfig := common.SendConfig{
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
		resp *simplexcommon.VerifiedReplicationResponse
	}{
		{
			name: "nil latest round",
			resp: &simplexcommon.VerifiedReplicationResponse{
				Data: []simplexcommon.VerifiedQuorumRound{
					{
						VerifiedBlock: &Block{
							metadata: simplexcommon.ProtocolMetadata{},
							vmBlock:  snowmantest.Genesis,
						},
					},
				},
				LatestRound: nil,
			},
		},
		{
			name: "empty seqs",
			resp: &simplexcommon.VerifiedReplicationResponse{
				Data:        []simplexcommon.VerifiedQuorumRound{},
				LatestRound: nil,
			},
		},
		{
			name: "non-nil latest round",
			resp: &simplexcommon.VerifiedReplicationResponse{
				Data: []simplexcommon.VerifiedQuorumRound{},
				LatestRound: &simplexcommon.VerifiedQuorumRound{
					VerifiedBlock: &Block{
						metadata: simplexcommon.ProtocolMetadata{},
						vmBlock:  snowmantest.Genesis,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, newReplicationResponse(chainID, tt.resp))
		})
	}
}
