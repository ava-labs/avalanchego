// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package oracle

import "context"

// OracleEvent bundles the parsed message with the raw justification (e.g. a
// Solana tx signature) that the sidecar needs to look up and verify the event.
// Justification is not part of the signed output.
type OracleEvent struct {
	Message       *OracleMessage
	Justification []byte
}

// SidecarClient implementations must be safe for concurrent use.
type SidecarClient interface {
	Verify(ctx context.Context, event *OracleEvent) error
}
