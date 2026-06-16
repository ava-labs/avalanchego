// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package external

import "context"

// ExternalEvent combines the signed message content with the relayer-supplied
// justification. It is passed to the sidecar for verification.
//
// Message contains what is being attested to and will be signed.
// Justification carries lookup hints (e.g. a Solana transaction signature or
// slot number) that help the sidecar verify but are not part of the signed
// output and are never seen by the on-chain adapter.
type ExternalEvent struct {
	Message       *ExternalMessage
	Justification []byte
}

// SidecarClient is the interface the ExternalChainVerifier uses to delegate
// verification decisions to the local sidecar process. The sidecar is
// responsible for all chain-specific logic: connecting to external RPCs,
// parsing chain-specific data formats, and applying the configured validation
// rules.
//
// Verify returns nil if the event passes all configured validation rules, or a
// non-nil error describing the first failure.
type SidecarClient interface {
	Verify(ctx context.Context, event *ExternalEvent) error
}
