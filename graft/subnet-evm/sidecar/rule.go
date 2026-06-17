// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sidecar

import (
	"context"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/external"
)

// ValidationRule is a single independently-evaluable check that an external
// event occurred on the source chain.
//
// Validate returns (true, nil) if the check passes conclusively, (false, nil)
// if the check fails conclusively (e.g. transaction not found), or
// (false, err) if the check could not be completed due to an infrastructure
// failure such as an unreachable RPC endpoint.
//
// The distinction between false-nil and false-err matters to
// ExternalInteropValidationRule: an infrastructure error in a quorum rule is
// treated as abstention rather than a vote against, so a quorum can still be
// reached by other rules if enough of them succeed.
type ValidationRule interface {
	Validate(ctx context.Context, event *external.ExternalEvent) (bool, error)
}
