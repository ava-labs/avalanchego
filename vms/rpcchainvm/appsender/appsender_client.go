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
	// called if any method calls return a non-nil error
	// TODO is this how we should handle errors?
	onErrorF func()
}

// NewClient returns a client that is connected to a remote AppSender.
// If any method returns an error, calls [onErrorF].
func NewClient(client appsenderproto.AppSenderClient, onErrorF func()) *Client {
	return &Client{client: client}
}

func (c *Client) SendAppRequest(nodeIDs ids.ShortSet, requestID uint32, request []byte) {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID // Prevent overwrite in next iteration
		nodeIDsBytes[i] = nodeID[:]
		i++
	}
	_, err := c.client.SendAppRequest(
		context.Background(),
		&appsenderproto.SendAppRequestMsg{
			NodeIDs:   nodeIDsBytes,
			RequestID: requestID,
			Request:   request,
		},
	)
	if err != nil {
		c.onErrorF()
	}
}

func (c *Client) SendAppResponse(nodeID ids.ShortID, requestID uint32, response []byte) {
	_, err := c.client.SendAppResponse(
		context.Background(),
		&appsenderproto.SendAppResponseMsg{
			NodeID:    nodeID[:],
			RequestID: requestID,
			Response:  response,
		},
	)
	if err != nil {
		c.onErrorF()
	}
}

func (c *Client) SendAppGossip(msg []byte) {
	_, err := c.client.SendAppGossip(
		context.Background(),
		&appsenderproto.SendAppGossipMsg{
			Msg: msg,
		},
	)
	if err != nil {
		c.onErrorF()
	}
}
