// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

func GetNodeURIs(nodes []*Node) []NodeURI {
	uris := make([]NodeURI, 0, len(nodes))
	for _, node := range nodes {
		if node.IsEphemeral {
			// Avoid returning URIs for nodes whose lifespan is indeterminate
			continue
		}
		// Only append URIs that are not empty. A node may have an
		// empty URI if it is not currently running.
		if len(node.URI) > 0 {
			uris = append(uris, NodeURI{
				NodeID: node.NodeID,
				URI:    node.URI,
			})
		}
	}
	return uris
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
