// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/ethapi"
)

// SendTx implements the respective method of [ethapi.Backend], accepting
// transactions submitted via the `eth_sendTransaction` RPC method. Unlike
// [gossip.BloomSet.Add], transactions added via this method will not be added
// to the Bloom filter, but will be push-gossiped; see
// [Set.RegisterPushGossiper].
func (s *Set) SendTx(ctx context.Context, ethTx *types.Transaction) error {
	var _ ethapi.Backend // protect the import for [comment] rendering

	// Note: the Bloom filter is only necessary for pull gossip, which is only
	// done between validator nodes. Validators are encouraged to NOT expose RPC
	// APIs (i.e. this method) and SHOULD therefore only add transactions via
	// [gossip.BloomSet.Add]. Hence there is a MECE separation between
	// pull+Bloom vs API+push under recommended usage patterns.

	if err := errors.Join(s.set.addToPool(true /*local*/, ethTx)...); err != nil {
		return err
	}
	for _, add := range s.pushTo {
		add(Transaction{ethTx})
	}
	return nil
}

// RegisterPushGossiper registers [gossip.PushGossiper.Add] as a callback for
// every transaction received via [Set.SendTx].
//
// NOTE: it is not safe to call this method concurrently with [Set.SendTx];
// registration is expected to occur immediately after construction of the
// [Set].
func (s *Set) RegisterPushGossiper(push *gossip.PushGossiper[Transaction]) {
	s.pushTo = append(s.pushTo, push.Add)
}
