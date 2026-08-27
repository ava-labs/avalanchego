// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warp handles the storage and signature-request verification of
// Avalanche Warp Messages for SAE-based VMs. Chain-specific behaviour —
// message extraction from receipts, predicate contexts, and additional
// signable payload types — is injected by the chain packages
// (vms/saevm/cchain/warp and vms/saevm/subnetevm/warp).
package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var _ acp118.Verifier = (*Verifier)(nil)

// Verifier verifies that this node should sign a warp message.
type Verifier struct {
	backend       Backend
	storage       *Storage
	addressedCall AddressedCallVerifier
}

// NewVerifier returns an ACP-118 message verifier. `addressedCall` MAY be nil,
// in which case [payload.AddressedCall] payloads are rejected with
// [UnknownMessageErrCode].
func NewVerifier(backend Backend, storage *Storage, addressedCall AddressedCallVerifier) *Verifier {
	return &Verifier{
		backend:       backend,
		storage:       storage,
		addressedCall: addressedCall,
	}
}

// Backend that the [Verifier] depends on to look for accepted blocks.
type Backend interface {
	// IsAccepted returns a non-nil error if the block with the given ID is not
	// accepted.
	IsAccepted(ctx context.Context, blockID ids.ID) error
}

// An AddressedCallVerifier decides whether this node should sign a
// [payload.AddressedCall] message that is not already in [Storage] — e.g.
// subnet-evm's validator-uptime attestations. Implementations SHOULD return
// error codes outside the range reserved by this package.
type AddressedCallVerifier interface {
	VerifyAddressedCall(*payload.AddressedCall) *common.AppError
}

// The error codes are returned by [Verifier.Verify] to identify why a message
// was not signed.
const (
	StorageErrCode = iota + 1
	ParseErrCode
	UnknownMessageErrCode
	NotAcceptedErrCode
)

// Verify verifies that this node should sign m.
func (v *Verifier) Verify(ctx context.Context, m *warp.UnsignedMessage, _ []byte) *common.AppError {
	// If the message was sent by the precompile or registered as an off-chain
	// message, it will be available in storage.
	_, err := v.storage.Get(m.ID())
	if err == nil { // if NO error
		return nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return &common.AppError{
			Code:    StorageErrCode,
			Message: "loading message: " + err.Error(),
		}
	}

	// Block acceptance doesn't go through the precompile, so we need to check
	// whether the message is for an accepted block.
	p, err := payload.Parse(m.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "parsing payload: " + err.Error(),
		}
	}

	switch p := p.(type) {
	case *payload.Hash:
		if err := v.backend.IsAccepted(ctx, p.Hash); err != nil {
			return &common.AppError{
				Code:    NotAcceptedErrCode,
				Message: "block not marked as accepted: " + err.Error(),
			}
		}
		return nil
	case *payload.AddressedCall:
		if v.addressedCall != nil {
			return v.addressedCall.VerifyAddressedCall(p)
		}
	}
	return &common.AppError{
		Code:    UnknownMessageErrCode,
		Message: fmt.Sprintf("unknown %T message", p),
	}
}
