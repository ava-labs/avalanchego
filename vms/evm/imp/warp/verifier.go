// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warp handles the parsing, verification, and storage of Avalanche Warp
// Messages for SAE-based VMs.
package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/libevm/options"

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

// An Option overrides default [Verifier] behavior.
type Option = options.Option[verifierConfig]

type verifierConfig struct {
	addressedCall AddressedCallVerifier
}

// WithAddressedCallVerifier sets the [AddressedCallVerifier] consulted for
// [payload.AddressedCall] messages that are not in [Storage]. By default, such
// messages are rejected with [UnknownMessageErrCode].
func WithAddressedCallVerifier(v AddressedCallVerifier) Option {
	return options.Func[verifierConfig](func(c *verifierConfig) {
		c.addressedCall = v
	})
}

// NewVerifier returns an ACP-118 message verifier.
func NewVerifier(backend Backend, storage *Storage, opts ...Option) *Verifier {
	cfg := options.ApplyTo(&verifierConfig{
		addressedCall: rejectAddressedCalls{},
	}, opts...)
	return &Verifier{
		backend:       backend,
		storage:       storage,
		addressedCall: cfg.addressedCall,
	}
}

// Backend that the [Verifier] depends on to look for accepted blocks. It is
// satisfied by [sae.VM].
type Backend interface {
	// IsAcceptedBlock returns a non-nil error if the block with the given ID
	// is not accepted.
	IsAcceptedBlock(ctx context.Context, blockID ids.ID) error
}

// An AddressedCallVerifier decides whether this node should sign a
// [payload.AddressedCall] message that is not already in [Storage] — e.g.
// the L1 VM's validator-uptime attestations. Implementations MUST number their
// error codes from [NextErrCode] so that they are distinct from this package's.
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
	// NextErrCode is the lowest error code not used by this package.
	NextErrCode
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
		if err := v.backend.IsAcceptedBlock(ctx, p.Hash); err != nil {
			return &common.AppError{
				Code:    NotAcceptedErrCode,
				Message: "block not marked as accepted: " + err.Error(),
			}
		}
		return nil
	case *payload.AddressedCall:
		return v.addressedCall.VerifyAddressedCall(p)
	}
	return unknownMessage(p)
}

var _ AddressedCallVerifier = rejectAddressedCalls{}

// rejectAddressedCalls rejects every [payload.AddressedCall] with
// [UnknownMessageErrCode].
type rejectAddressedCalls struct{}

func (rejectAddressedCalls) VerifyAddressedCall(p *payload.AddressedCall) *common.AppError {
	return unknownMessage(p)
}

func unknownMessage(p payload.Payload) *common.AppError {
	return &common.AppError{
		Code:    UnknownMessageErrCode,
		Message: fmt.Sprintf("unknown %T message", p),
	}
}
