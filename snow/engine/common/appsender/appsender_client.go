// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"

	appsenderpb "github.com/ava-labs/avalanchego/proto/pb/appsender"
)

var _ common.AppSender = &Client{}

type Client struct {
	client appsenderpb.AppSenderClient
}

// NewClient returns a client that is connected to a remote AppSender.
func NewClient(client appsenderpb.AppSenderClient) *Client {
	return &Client{client: client}
}

func (c *Client) SendAppRequest(nodeIDs ids.NodeIDSet, requestID uint32, request []byte) error {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID // Prevent overwrite in next iteration
		nodeIDsBytes[i] = nodeID[:]
		i++
	}
	_, err := c.client.SendAppRequest(
		context.Background(),
		&appsenderpb.SendAppRequestMsg{
			NodeIds:   nodeIDsBytes,
			RequestId: requestID,
			Request:   request,
		},
	)
	return err
}

func (c *Client) SendAppResponse(nodeID ids.NodeID, requestID uint32, response []byte) error {
	_, err := c.client.SendAppResponse(
		context.Background(),
		&appsenderpb.SendAppResponseMsg{
			NodeId:    nodeID[:],
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (c *Client) SendAppGossip(msg []byte) error {
	_, err := c.client.SendAppGossip(
		context.Background(),
		&appsenderpb.SendAppGossipMsg{
			Msg: msg,
		},
	)
	return err
}

func (c *Client) SendAppGossipSpecific(nodeIDs ids.NodeIDSet, msg []byte) error {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID // Prevent overwrite in next iteration
		nodeIDsBytes[i] = nodeID[:]
		i++
	}
	_, err := c.client.SendAppGossipSpecific(
		context.Background(),
		&appsenderpb.SendAppGossipSpecificMsg{
			NodeIds: nodeIDsBytes,
			Msg:     msg,
		},
	)
	return err
}

func (c *Client) SendCrossChainAppRequest(nodeIDs ids.NodeIDSet, sourceChainID ids.ID, destinationChainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID // Prevent overwrite in next iteration
		nodeIDsBytes[i] = nodeID[:]
		i++
	}
	_, err := c.client.SendCrossChainAppRequest(
		context.Background(),
		&appsenderpb.SendCrossChainAppRequestMsg{
			NodeIds:            nodeIDsBytes,
			SourceChainId:      sourceChainID[:],
			DestinationChainId: destinationChainID[:],
			RequestId:          requestID,
			Request:            appRequestBytes,
		},
	)
	return err
}

func (c *Client) SendCrossChainAppResponse(nodeID ids.NodeID, requestID uint32, sourceChainID ids.ID, destinationChainID ids.ID, appResponseBytes []byte) error {
	_, err := c.client.SendCrossChainAppResponse(
		context.Background(),
		&appsenderpb.SendCrossChainAppResponseMsg{
			NodeId:             nodeID[:],
			SourceChainId:      sourceChainID[:],
			DestinationChainId: destinationChainID[:],
			RequestId:          requestID,
			Response:           appResponseBytes,
		},
	)
	return err
}

func (c *Client) SendCrossChainAppGossip(sourceChainID ids.ID, destinationChainID ids.ID, appGossipBytes []byte) error {
	_, err := c.client.SendCrossChainAppGossip(
		context.Background(),
		&appsenderpb.SendCrossChainAppGossipMsg{
			SourceChainId:      sourceChainID[:],
			DestinationChainId: destinationChainID[:],
			Msg:                appGossipBytes,
		},
	)
	return err
}

func (c *Client) SendCrossChainAppGossipSpecific(nodeIDs ids.NodeIDSet, sourceChainID ids.ID, destinationChainID ids.ID, appGossipBytes []byte) error {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID // Prevent overwrite in next iteration
		nodeIDsBytes[i] = nodeID[:]
		i++
	}
	_, err := c.client.SendCrossChainAppGossipSpecific(
		context.Background(),
		&appsenderpb.SendCrossChainAppGossipSpecificMsg{
			NodeIds:            nodeIDsBytes,
			SourceChainId:      sourceChainID[:],
			DestinationChainId: destinationChainID[:],
			Msg:                appGossipBytes,
		},
	)
	return err
}
