// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
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

var errParsingWarpMessage = errors.New("parsing warp message")

// ParseOffChainMessages parses operator-configured off-chain warp messages
// (unrelated to any on-chain event) that the node should be willing to sign,
// for use as [NewStorage] overrides.
func ParseOffChainMessages(encoded []hexutil.Bytes) ([]*warp.UnsignedMessage, error) {
	msgs := make([]*warp.UnsignedMessage, len(encoded))
	for i, bytes := range encoded {
		msg, err := warp.ParseUnsignedMessage(bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: at index %d: %w", errParsingWarpMessage, i, err)
		}
		msgs[i] = msg
	}
	return msgs, nil
}
