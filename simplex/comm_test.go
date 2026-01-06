// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"
	"time"

	"github.com/ava-labs/simplex"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendermock"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
)

var testSimplexMessage = simplex.Message{
	VoteMessage: &simplex.Vote{
		Vote: simplex.ToBeSignedVote{
			BlockHeader: simplex.BlockHeader{
				ProtocolMetadata: simplex.ProtocolMetadata{
					Version: 1,
					Epoch:   1,
					Round:   1,
					Seq:     1,
				},
			},
		},
		Signature: simplex.Signature{
			Signer: []byte("dummy_node_id"),
			Value:  []byte("dummy_signature"),
		},
	},
}

func TestCommSendMessage(t *testing.T) {
	config := newEngineConfig(t, 1)

	destinationNodeID := ids.GenerateTestNodeID()
	ctrl := gomock.NewController(t)
	sender := sendermock.NewExternalSender(ctrl)
	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)

	config.OutboundMsgBuilder = mc
	config.Sender = sender

	comm, err := NewComm(config)
	require.NoError(t, err)

	outboundMsg, err := mc.SimplexMessage(newVote(config.Ctx.ChainID, testSimplexMessage.VoteMessage))
	require.NoError(t, err)
	expectedSendConfig := common.SendConfig{
		NodeIDs: set.Of(destinationNodeID),
	}
	sender.EXPECT().Send(outboundMsg, expectedSendConfig, comm.subnetID, gomock.Any())

	comm.Send(&testSimplexMessage, destinationNodeID[:])
}

// TestCommBroadcast tests the Broadcast method sends to all nodes in the subnet
// not including the sending node.
func TestCommBroadcast(t *testing.T) {
	config := newEngineConfig(t, 3)

	ctrl := gomock.NewController(t)
	sender := sendermock.NewExternalSender(ctrl)
	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)

	config.OutboundMsgBuilder = mc
	config.Sender = sender

	comm, err := NewComm(config)
	require.NoError(t, err)
	outboundMsg, err := mc.SimplexMessage(newVote(config.Ctx.ChainID, testSimplexMessage.VoteMessage))
	require.NoError(t, err)
	nodes := make([]ids.NodeID, 0, len(comm.Nodes()))
	for _, node := range comm.Nodes() {
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

	ctrl := gomock.NewController(t)
	mc, err := message.NewCreator(
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)
	sender := sendermock.NewExternalSender(ctrl)

	config.OutboundMsgBuilder = mc
	config.Sender = sender

	// set the curNode to a different nodeID than the one in the config
	vdrs := generateTestNodes(t, 3)
	config.Validators = newTestValidatorInfo(vdrs)

	_, err = NewComm(config)
	require.ErrorIs(t, err, errNodeNotFound)
}

func TestSimplexMessageReplicationResponse(t *testing.T) {
	chainID := ids.GenerateTestID()
	tests := []struct {
		name string
		resp *simplex.VerifiedReplicationResponse
	}{
		{
			name: "nil latest round",
			resp: &simplex.VerifiedReplicationResponse{
				Data: []simplex.VerifiedQuorumRound{
					{
						VerifiedBlock: &Block{
							metadata: simplex.ProtocolMetadata{},
							vmBlock:  snowmantest.Genesis,
						},
					},
				},
				LatestRound: nil,
			},
		},
		{
			name: "empty seqs",
			resp: &simplex.VerifiedReplicationResponse{
				Data:        []simplex.VerifiedQuorumRound{},
				LatestRound: nil,
			},
		},
		{
			name: "non-nil latest round",
			resp: &simplex.VerifiedReplicationResponse{
				Data: []simplex.VerifiedQuorumRound{},
				LatestRound: &simplex.VerifiedQuorumRound{
					VerifiedBlock: &Block{
						metadata: simplex.ProtocolMetadata{},
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
