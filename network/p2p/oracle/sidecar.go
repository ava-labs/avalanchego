// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package oracle

import "context"

// OracleEvent is the unit of work passed to a SidecarClient for verification.
// Message is the parsed payload. Justification carries lookup hints (e.g. a
// Solana transaction signature) that the sidecar needs to locate and verify the
// event on the source chain, but that are not part of the signed output.
type OracleEvent struct {
	Message       *OracleMessage
	Justification []byte
}

// SidecarClient is implemented by whatever process queries the source chain.
// The simplest implementation does a single RPC call (e.g. Solana getTransaction)
// and returns nil if the event matches, non-nil otherwise.
//
// Implementations must be safe for concurrent use.
type SidecarClient interface {
	Verify(ctx context.Context, event *OracleEvent) error
}
