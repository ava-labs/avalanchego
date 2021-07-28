// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/appsender/appsenderproto"
)

var _ common.AppSender = &Client{}

type Client struct {
	client appsenderproto.AppSenderClient
}

// NewClient returns a client that is connected to a remote AppSender
func NewClient(client appsenderproto.AppSenderClient) *Client {
	return &Client{client: client}
}

// TODO handle the error
func (c *Client) SendAppRequest(nodeIDs ids.ShortSet, requestID uint32, request []byte) {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID // Prevent overwrite in next iteration
		nodeIDsBytes[i] = nodeID[:]
		i++
	}
	_, _ = c.client.SendAppRequest(
		context.Background(),
		&appsenderproto.SendAppRequestMsg{
			NodeIDs:   nodeIDsBytes,
			RequestID: requestID,
			Request:   request,
		},
	)
}

// TODO handle the error
func (c *Client) SendAppResponse(nodeID ids.ShortID, requestID uint32, response []byte) {
	_, _ = c.client.SendAppResponse(
		context.Background(),
		&appsenderproto.SendAppResponseMsg{
			NodeID:    nodeID[:],
			RequestID: requestID,
			Response:  response,
		},
	)
}

// TODO handle the error
func (c *Client) SendAppGossip(msg []byte) {
	_, _ = c.client.SendAppGossip(
		context.Background(),
		&appsenderproto.SendAppGossipMsg{
			Msg: msg,
		},
	)
}
