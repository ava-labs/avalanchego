// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = (*client)(nil)

// Client interface for interacting with the IPCS endpoint
type Client interface {
	PublishBlockchain(string) (*PublishBlockchainReply, error)
	UnpublishBlockchain(string) (bool, error)
	GetPublishedBlockchains() ([]ids.ID, error)
}

// Client implementation for interacting with the IPCS endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with the IPCS endpoint
func NewClient(uri string, requestTimeout time.Duration) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/ipcs", "ipcs", requestTimeout),
	}
}

// PublishBlockchain requests the node to begin publishing consensus and decision events
func (c *client) PublishBlockchain(blockchainID string) (*PublishBlockchainReply, error) {
	res := &PublishBlockchainReply{}
	err := c.requester.SendRequest("publishBlockchain", &PublishBlockchainArgs{
		BlockchainID: blockchainID,
	}, res)
	return res, err
}

// UnpublishBlockchain requests the node to stop publishing consensus and decision events
func (c *client) UnpublishBlockchain(blockchainID string) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("unpublishBlockchain", &UnpublishBlockchainArgs{
		BlockchainID: blockchainID,
	}, res)
	return res.Success, err
}

// GetPublishedBlockchains requests the node to get blockchains being published
func (c *client) GetPublishedBlockchains() ([]ids.ID, error) {
	res := &GetPublishedBlockchainsReply{}
	err := c.requester.SendRequest("getPublishedBlockchains", nil, res)
	return res.Chains, err
}
