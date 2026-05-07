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

const (
	StorageErrCode = iota + 1
	ParseErrCode
	TypeErrCode
	VerifyErrCode
)

var _ acp118.Verifier = (*Verifier)(nil)

type BlockClient interface {
	IsAccepted(ctx context.Context, blockID ids.ID) error
}

// Verifier verifies whether this node should be willing to sign a warp message.
type Verifier struct {
	blocks  BlockClient
	storage *Storage
}

// NewVerifier returns an ACP-118 message verifier.
func NewVerifier(blocks BlockClient, storage *Storage) *Verifier {
	return &Verifier{
		blocks:  blocks,
		storage: storage,
	}
}

// Verify verifies that this node should sign the unsigned warp message.
func (v *Verifier) Verify(ctx context.Context, m *warp.UnsignedMessage, _ []byte) *common.AppError {
	id := m.ID()
	_, err := v.storage.GetMessage(id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return &common.AppError{
			Code:    StorageErrCode,
			Message: fmt.Sprintf("failed to get message %s: %s", id, err.Error()),
		}
	}

	parsed, err := payload.Parse(m.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "failed to parse payload: " + err.Error(),
		}
	}

	p, ok := parsed.(*payload.Hash)
	if !ok {
		return &common.AppError{
			Code:    TypeErrCode,
			Message: fmt.Sprintf("wrong payload type: %T", p),
		}
	}

	if err := v.blocks.IsAccepted(ctx, p.Hash); err != nil {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("failed to get block %s: %s", p.Hash, err.Error()),
		}
	}
	return nil
}
