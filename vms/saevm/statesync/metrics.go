// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"github.com/prometheus/client_golang/prometheus"

	syncnet "github.com/ava-labs/avalanchego/vms/evm/sync/network"
)

// metricsNamespace prefixes every state sync metric, the client-side request
// counts and the server-side handler counts alike. The C-Chain's atomic trie
// counterpart is registered under its own namespace; see
// vms/saevm/cchain/statesync.
const metricsNamespace = "statesync"

// clientMetrics counts the requests this node sends while state syncing, one
// [syncnet.Metrics] per RPC type. The base names mirror coreth's client-side
// sync metrics.
type clientMetrics struct {
	stateTrieLeaves *syncnet.Metrics
	code            *syncnet.Metrics
	blocks          *syncnet.Metrics
}

func newClientMetrics(reg prometheus.Registerer) (*clientMetrics, error) {
	stateTrieLeaves, err := syncnet.NewMetrics(reg, "sync_state_trie_leaves")
	if err != nil {
		return nil, err
	}
	code, err := syncnet.NewMetrics(reg, "sync_code")
	if err != nil {
		return nil, err
	}
	blocks, err := syncnet.NewMetrics(reg, "sync_blocks")
	if err != nil {
		return nil, err
	}
	return &clientMetrics{
		stateTrieLeaves: stateTrieLeaves,
		code:            code,
		blocks:          blocks,
	}, nil
}
