// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/avalanchego/version"
)

var (
	_ NetworkClient = &client{}

	ErrRequestFailed = errors.New("request failed")
)

// NetworkClient defines ability to send request / response through the Network
type NetworkClient interface {
	// RequestAny synchronously sends request to a randomly chosen peer with a
	// node version greater than or equal to minVersion.
	// Returns response bytes, the ID of the chosen peer, and ErrRequestFailed if
	// the request should be retried.
	RequestAny(minVersion *version.Application, request []byte) ([]byte, ids.NodeID, error)

	// Request synchronously sends request to the selected nodeID
	// Returns response bytes, and ErrRequestFailed if the request should be retried.
	Request(nodeID ids.NodeID, request []byte) ([]byte, error)

	// Gossip sends given gossip message to peers
	Gossip(gossip []byte) error

	// TrackBandwidth should be called for each valid request with the bandwidth
	// (length of response divided by request time), and with 0 if the response is invalid.
	TrackBandwidth(nodeID ids.NodeID, bandwidth float64)
}

// client implements NetworkClient interface
// provides ability to send request / responses through the Network
type client struct {
	network Network
}

// NewNetworkClient returns Client for a given network
func NewNetworkClient(network Network) NetworkClient {
	return &client{
		network: network,
	}
}

// RequestAny synchronously sends request to a randomly chosen peer with a
// node version greater than or equal to minVersion.
// Returns response bytes, the ID of the chosen peer, and ErrRequestFailed if
// the request should be retried.
func (c *client) RequestAny(minVersion *version.Application, request []byte) ([]byte, ids.NodeID, error) {
	waitingHandler := newWaitingResponseHandler()
	nodeID, err := c.network.RequestAny(minVersion, request, waitingHandler)
	if err != nil {
		return nil, nodeID, err
	}
	response := <-waitingHandler.responseChan
	if waitingHandler.failed {
		return nil, nodeID, ErrRequestFailed
	}
	return response, nodeID, nil
}

// Request synchronously sends request to the specified nodeID
// Returns response bytes and ErrRequestFailed if the request should be retried.
func (c *client) Request(nodeID ids.NodeID, request []byte) ([]byte, error) {
	waitingHandler := newWaitingResponseHandler()
	if err := c.network.Request(nodeID, request, waitingHandler); err != nil {
		return nil, err
	}
	response := <-waitingHandler.responseChan
	if waitingHandler.failed {
		return nil, ErrRequestFailed
	}
	return response, nil
}

func (c *client) Gossip(gossip []byte) error {
	return c.network.Gossip(gossip)
}

func (c *client) TrackBandwidth(nodeID ids.NodeID, bandwidth float64) {
	c.network.TrackBandwidth(nodeID, bandwidth)
}
