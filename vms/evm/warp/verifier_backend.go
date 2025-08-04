// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
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
	case *payload.AddressedCall:
		return b.verifyOffchainAddressedCall(p)
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

// verifyOffchainAddressedCall verifies the addressed call message
func (b *backend) verifyOffchainAddressedCall(addressedCall *payload.AddressedCall) *common.AppError {
	// Further, parse the payload to see if it is a known type.
	parsed, err := message.Parse(addressedCall.Payload)
	if err != nil {
		b.stats.IncMessageParseFail()
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "failed to parse addressed call message: " + err.Error(),
		}
	}

	if len(addressedCall.SourceAddress) != 0 {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: "source address should be empty for offchain addressed messages",
		}
	}

	switch p := parsed.(type) {
	case *message.ValidatorUptime:
		if err := b.verifyUptimeMessage(p); err != nil {
			b.stats.IncUptimeValidationFail()
			return err
		}
	default:
		b.stats.IncMessageParseFail()
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("unknown message type: %T", p),
		}
	}

	return nil
}

func (b *backend) verifyUptimeMessage(uptimeMsg *message.ValidatorUptime) *common.AppError {
	vdr, currentUptime, _, err := b.validatorReader.GetValidatorAndUptime(uptimeMsg.ValidationID)
	if err != nil {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("failed to get uptime for validationID %s: %s", uptimeMsg.ValidationID, err.Error()),
		}
	}

	currentUptimeSeconds := uint64(currentUptime.Seconds())
	// verify the current uptime against the total uptime in the message
	if currentUptimeSeconds < uptimeMsg.TotalUptime {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("current uptime %d is less than queried uptime %d for nodeID %s", currentUptimeSeconds, uptimeMsg.TotalUptime, vdr.NodeID),
		}
	}

	return nil
}
