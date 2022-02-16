// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/version"
)

var _ Client = &client{}

// Client defines ability to send request / response through the Network
type Client interface {
	// RequestAny synchronously sends request to the first connected peer that matches the specified minVersion in
	// random order.
	// A peer is considered a match if its version is greater than or equal to the specified minVersion
	// Returns errNoPeersMatchingVersion if no peer could be found matching specified version
	RequestAny(minVersion version.Application, request []byte) ([]byte, bool, error)

	// Gossip sends given gossip message to peers
	Gossip(gossip []byte) error
}

// client implements Client interface
// provides ability to send request / responses through the Network
type client struct {
	network Network
}

// RequestAny synchronously sends request to the first connected peer that matches the specified minVersion in
// random order.
// Returns response bytes, whether the request failed and optional error
// Returns errNoPeersMatchingVersion if no peer could be found matching specified version
// This function is blocks until a response is received from the peer
func (c *client) RequestAny(minVersion version.Application, request []byte) ([]byte, bool, error) {
	waitingHandler := newWaitingResponseHandler()
	if err := c.network.RequestAny(minVersion, request, waitingHandler); err != nil {
		return nil, true, err
	}
	return <-waitingHandler.responseChan, waitingHandler.failed, nil
}

func (c *client) Gossip(gossip []byte) error {
	return c.network.Gossip(gossip)
}

// NewClient returns Client for a given network
func NewClient(network Network) Client {
	return &client{
		network: network,
	}
}
