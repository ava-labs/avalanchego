// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// signatureCacheSize bounds the ACP-118 handler's cache of signed messages.
const signatureCacheSize = 512

// RegisterHandler registers an ACP-118 signature-request handler backed by
// `verifier` and `signer` on the network.
func RegisterHandler(net *network.Network, verifier acp118.Verifier, signer warp.Signer) error {
	handler := acp118.NewCachedHandler(
		lru.NewCache[ids.ID, []byte](signatureCacheSize),
		verifier,
		signer,
	)
	return net.AddHandler(acp118.HandlerID, handler)
}
