// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

const (
	DefaultNodeTickerInterval = 50 * time.Millisecond
)

var ErrUnrecoverableNodeHealthCheck = errors.New("failed to query node health")

func CheckNodeHealth(ctx context.Context, uri string) (*health.APIReply, error) {
	// Check that the node is reporting healthy
	healthReply, err := health.NewClient(uri).Health(ctx, nil)
	if err == nil {
		return healthReply, nil
	}

	switch t := err.(type) {
	case *net.OpError:
		if t.Op == "read" {
			// Connection refused - potentially recoverable
			return nil, err
		}
	case syscall.Errno:
		if t == syscall.ECONNREFUSED {
			// Connection refused - potentially recoverable
			return nil, err
		}
	}
	// Assume all other errors are not recoverable
	return nil, fmt.Errorf("%w: %w", ErrUnrecoverableNodeHealthCheck, err)
}

// NodeURI associates a node ID with its API URI.
type NodeURI struct {
	NodeID ids.NodeID
	URI    string
}

// GetNodeURIs returns the URIs of the provided nodes that are running and not ephemeral. The URIs returned
// are guaranteed be reachable by the caller until the cleanup function is called regardless of whether the
// nodes are running as local processes or in a kube cluster.
func GetNodeURIs(ctx context.Context, nodes []*Node, deferCleanupFunc func(func())) ([]NodeURI, error) {
	availableNodes := FilterAvailableNodes(nodes)
	uris := []NodeURI{}
	for _, node := range availableNodes {
		uri, cancel, err := node.GetLocalURI(ctx)
		if err != nil {
			return nil, err
		}
		deferCleanupFunc(cancel)
		uris = append(uris, NodeURI{
			NodeID: node.NodeID,
			URI:    uri,
		})
	}

	return uris, nil
}

// FilteredAvailableNodes filters the provided nodes by whether they are running and not ephemeral.
func FilterAvailableNodes(nodes []*Node) []*Node {
	filteredNodes := []*Node{}
	for _, node := range nodes {
		if node.IsEphemeral {
			// Avoid returning URIs for nodes whose lifespan is indeterminate
			continue
		}
		if !node.IsRunning() {
			// Only running nodes have URIs
			continue
		}
		filteredNodes = append(filteredNodes, node)
	}
	return filteredNodes
}

// GetNodeWebsocketURIs returns a list of websocket URIs for the given nodes and
// blockchain ID, in the form "ws://<node-uri>/ext/bc/<blockchain-id>/ws".
// Ephemeral and stopped nodes are ignored.
func GetNodeWebsocketURIs(
	ctx context.Context,
	nodes []*Node,
	blockchainID string,
	deferCleanupFunc func(func()),
) ([]string, error) {
	nodeURIs, err := GetNodeURIs(ctx, nodes, deferCleanupFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to get node URIs: %w", err)
	}
	wsURIs := make([]string, len(nodeURIs))
	for i := range nodeURIs {
		uri, err := url.Parse(nodeURIs[i].URI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node URI: %w", err)
		}
		uri.Scheme = "ws" // use websocket to be able to stream events
		wsURIs[i] = fmt.Sprintf("%s/ext/bc/%s/ws", uri, blockchainID)
	}
	return wsURIs, nil
}

// Marshal to json with default prefix and indent.
func DefaultJSONMarshal(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// Helper simplifying creation of a set of private keys
func NewPrivateKeys(keyCount int) ([]*secp256k1.PrivateKey, error) {
	keys := make([]*secp256k1.PrivateKey, 0, keyCount)
	for i := 0; i < keyCount; i++ {
		key, err := secp256k1.NewPrivateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate private key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func NodesToIDs(nodes ...*Node) []ids.NodeID {
	nodeIDs := make([]ids.NodeID, len(nodes))
	for i, node := range nodes {
		nodeIDs[i] = node.NodeID
	}
	return nodeIDs
}

func GetEnvWithDefault(envVar, defaultVal string) string {
	val := os.Getenv(envVar)
	if len(val) == 0 {
		return defaultVal
	}
	return val
}
