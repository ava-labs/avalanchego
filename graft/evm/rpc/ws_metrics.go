// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// This file is NOT derived from go-ethereum. It is first-party Ava Labs code
// holding the native-Prometheus metrics for the RPC server. Keeping all metrics
// code here (rather than in the upstream-derived rpc sources) keeps the merge
// surface against libevm/go-ethereum small: the derived files carry only tiny
// mechanical hooks that call into this file, so a future re-sync conflicts on a
// few call sites instead of rewritten bodies.
//
// See docs/design/websocket-rpc-visibility.md.

package rpc

import "github.com/prometheus/client_golang/prometheus"

// rpcMetrics holds the native-Prometheus metric vectors for the RPC server.
//
// Vectors are added per phase of the websocket-rpc-visibility design; Phase 0
// establishes only the struct and the plumbing that carries it from NewServer
// into the server (and, in later phases, the handler). No metrics are emitted
// yet.
type rpcMetrics struct {
	// reg is the registerer the metric vectors are registered against. It may be
	// nil (tests, or transports constructed without a metrics registry), in
	// which case registration is skipped.
	reg prometheus.Registerer //nolint:unused // plumbed in Phase 0; first read in Phase 1, which registers the metric vectors
}

// newRPCMetrics builds the RPC metrics set and registers its vectors against
// reg. A nil reg is allowed and yields a metrics set that registers nothing, so
// callers and the request hot path never need a nil check.
func newRPCMetrics(reg prometheus.Registerer) *rpcMetrics {
	return &rpcMetrics{reg: reg}
}
