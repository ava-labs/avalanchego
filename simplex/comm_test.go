// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message/messagemock"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendermock"
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

func TestComm(t *testing.T) {
	config, err := newEngineConfig(1)
	require.NoError(t, err)

	destinationNodeID := ids.GenerateTestNodeID()

	ctrl := gomock.NewController(t)
	msgCreator := messagemock.NewOutboundMsgBuilder(ctrl)
	sender := sendermock.NewExternalSender(ctrl)

	config.OutboundMsgBuilder = msgCreator
	config.Sender = sender

	comm, err := NewComm(config)
	require.NoError(t, err)

	msgCreator.EXPECT().Vote(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil)
	sender.EXPECT().Send(gomock.Any(), gomock.Any(), comm.subnetID, gomock.Any())

	comm.SendMessage(&testSimplexMessage, destinationNodeID[:])
}

// TestCommBroadcast tests the Broadcast method sends to all nodes in the subnet
// not including the sending node.
func TestCommBroadcast(t *testing.T) {
	config, err := newEngineConfig(3)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	msgCreator := messagemock.NewOutboundMsgBuilder(ctrl)
	sender := sendermock.NewExternalSender(ctrl)

	config.OutboundMsgBuilder = msgCreator
	config.Sender = sender

	comm, err := NewComm(config)
	require.NoError(t, err)

	msgCreator.EXPECT().Vote(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, nil).Times(2)
	sender.EXPECT().Send(gomock.Any(), gomock.Any(), comm.subnetID, gomock.Any()).Times(2)

	comm.Broadcast(&testSimplexMessage)
}

func TestCommFailsWithoutCurrentNode(t *testing.T) {
	config, err := newEngineConfig(3)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	msgCreator := messagemock.NewOutboundMsgBuilder(ctrl)
	sender := sendermock.NewExternalSender(ctrl)

	config.OutboundMsgBuilder = msgCreator
	config.Sender = sender

	// set the curNode to a different nodeID than the one in the config
	vdrs, err := generateTestValidators(3)
	require.NoError(t, err)
	config.Validators = newTestValidators(vdrs)

	_, err = NewComm(config)
	require.ErrorIs(t, err, errNodeNotFound)
}
