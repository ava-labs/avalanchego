// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const (
	ParseErrCode = iota + 1
	VerifyErrCode
)

// Verify verifies the signature of the message
// It also implements the acp118.Verifier interface
func (b *backend) Verify(ctx context.Context, unsignedMessage *avalancheWarp.UnsignedMessage, _ []byte) *common.AppError {
	messageID := unsignedMessage.ID()
	// Known on-chain messages should be signed
	if _, err := b.GetMessage(messageID); err == nil {
		return nil
	} else if err != database.ErrNotFound {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("failed to get message %s: %s", messageID, err.Error()),
		}
	}

	parsed, err := payload.Parse(unsignedMessage.Payload)
	if err != nil {
		b.stats.IncMessageParseFail()
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "failed to parse payload: " + err.Error(),
		}
	}

	switch p := parsed.(type) {
	case *payload.Hash:
		return b.verifyBlockMessage(ctx, p)
	default:
		b.stats.IncMessageParseFail()
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("unknown payload type: %T", p),
		}
	}
}

// verifyBlockMessage returns nil if blockHashPayload contains the ID
// of an accepted block indicating it should be signed by the VM.
func (b *backend) verifyBlockMessage(ctx context.Context, blockHashPayload *payload.Hash) *common.AppError {
	blockID := blockHashPayload.Hash
	_, err := b.blockClient.GetAcceptedBlock(ctx, blockID)
	if err != nil {
		b.stats.IncBlockValidationFail()
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("failed to get block %s: %s", blockID, err.Error()),
		}
	}

	return nil
}
