// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"errors"

	"github.com/ava-labs/libevm/accounts"
	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/libevm/ethapi"

	"github.com/ava-labs/avalanchego/vms/saevm/gasprice"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip"
)

// The GethBackends interface is the union of geth interfaces required by
// their implementations of JSON-RPC namespace handlers.
type GethBackends interface {
	ethapi.Backend
	tracers.Backend
	filters.BloomOverrider
	filters.Backend
}

// GethBackends returns the [GethBackends] that back all JSON-RPC namespace
// handlers registered by [Provider.Server].
func (p *Provider) GethBackends() GethBackends {
	return p.backend
}

var _ GethBackends = (*backend)(nil)

type backend struct {
	Chain
	config         Config
	accountManager *accounts.Manager

	*gasprice.Estimator
	*txgossip.Set
	chainIndexer
	bloomOverrider
	*bloomIndexer
}

func (b *backend) close() error {
	return errors.Join(
		b.accountManager.Close(),
		b.Estimator.Close(),
		b.bloomIndexer.Close(),
	)
}
