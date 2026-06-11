// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

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
	backend Backend
	storage *Storage
}

// NewVerifier returns an ACP-118 message verifier.
func NewVerifier(backend Backend, storage *Storage) *Verifier {
	return &Verifier{
		backend: backend,
		storage: storage,
	}
}

// Backend that the [Verifier] depends on to look for accepted blocks.
type Backend interface {
	// IsAccepted returns a non-nil error if the block with the given ID is not
	// accepted.
	IsAccepted(ctx context.Context, blockID ids.ID) error
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

	hash, ok := p.(*payload.Hash)
	if !ok {
		return &common.AppError{
			Code:    UnknownMessageErrCode,
			Message: fmt.Sprintf("unknown %T message", p),
		}
	}

	if err := v.backend.IsAccepted(ctx, hash.Hash); err != nil {
		return &common.AppError{
			Code:    NotAcceptedErrCode,
			Message: "block not marked as accepted: " + err.Error(),
		}
	}
	return nil
}
