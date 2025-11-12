// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/metrics"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/evm/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const (
	ParseErrCode = iota + 1
	VerifyErrCode
)

var _ acp118.Verifier = (*Verifier)(nil)

// BlockStore provides access to accepted blocks.
type BlockStore interface {
	GetBlock(ctx context.Context, blockID ids.ID) error
}

// DB stores and retrieves warp messages from the underlying database.
type DB struct {
	db database.Database
}

// NewDB creates a new warp message database.
func NewDB(db database.Database) *DB {
	return &DB{
		db: db,
	}
}

// Add stores a warp message in the database and cache.
func (d *DB) Add(unsignedMsg *warp.UnsignedMessage) error {
	msgID := unsignedMsg.ID()

	// In the case when a node restarts, and possibly changes its bls key, the cache gets emptied but the database does not.
	// So to avoid having incorrect signatures saved in the database after a bls key change, we save the full message in the database.
	// Whereas for the cache, after the node restart, the cache would be emptied so we can directly save the signatures.
	if err := d.db.Put(msgID[:], unsignedMsg.Bytes()); err != nil {
		return fmt.Errorf("failed to put warp message in db: %w", err)
	}

	return nil
}

// Get retrieves a warp message from the database.
func (d *DB) Get(msgID ids.ID) (*warp.UnsignedMessage, error) {
	unsignedMessageBytes, err := d.db.Get(msgID[:])
	if err != nil {
		return nil, err
	}

	unsignedMessage, err := warp.ParseUnsignedMessage(unsignedMessageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse unsigned message %s: %w", msgID.String(), err)
	}

	return unsignedMessage, nil
}

// Signer signs warp messages.
type Signer struct {
	warpSigner warp.Signer
}

// NewSigner creates a new warp message signer.
func NewSigner(warpSigner warp.Signer) *Signer {
	return &Signer{
		warpSigner: warpSigner,
	}
}

// Sign signs a warp message.
// Callers are responsible for verification and caching.
func (s *Signer) Sign(msg *warp.UnsignedMessage) ([]byte, error) {
	sig, err := s.warpSigner.Sign(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to sign warp message: %w", err)
	}
	return sig, nil
}

// Verifier implements acp118.Verifier and validates whether a warp message should be signed.
type Verifier struct {
	db            *DB
	blockClient   BlockStore
	uptimeTracker *uptimetracker.UptimeTracker

	// Metrics
	messageParseFail            metrics.Counter
	addressedCallValidationFail metrics.Counter
	blockValidationFail         metrics.Counter
	uptimeValidationFail        metrics.Counter
}

// NewVerifier creates a new warp message verifier.
func NewVerifier(
	db *DB,
	blockClient BlockStore,
	uptimeTracker *uptimetracker.UptimeTracker,
) *Verifier {
	return &Verifier{
		db:                          db,
		blockClient:                 blockClient,
		uptimeTracker:               uptimeTracker,
		messageParseFail:            metrics.NewRegisteredCounter("warp_backend_message_parse_fail", nil),
		addressedCallValidationFail: metrics.NewRegisteredCounter("warp_backend_addressed_call_validation_fail", nil),
		blockValidationFail:         metrics.NewRegisteredCounter("warp_backend_block_validation_fail", nil),
		uptimeValidationFail:        metrics.NewRegisteredCounter("warp_backend_uptime_validation_fail", nil),
	}
}

// Verify implements acp118.Verifier and validates whether a warp message should be signed.
func (v *Verifier) Verify(ctx context.Context, unsignedMessage *warp.UnsignedMessage, _ []byte) *common.AppError {
	messageID := unsignedMessage.ID()
	// Known on-chain messages should be signed
	if _, err := v.db.Get(messageID); err == nil {
		return nil
	} else if err != database.ErrNotFound {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("failed to get message %s: %s", messageID, err),
		}
	}

	parsed, err := payload.Parse(unsignedMessage.Payload)
	if err != nil {
		v.messageParseFail.Inc(1)
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("failed to parse payload: %s", err),
		}
	}

	switch p := parsed.(type) {
	case *payload.AddressedCall:
		return v.verifyOffchainAddressedCall(p)
	case *payload.Hash:
		return v.verifyBlockMessage(ctx, p)
	default:
		v.messageParseFail.Inc(1)
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("unknown payload type: %T", p),
		}
	}
}

// verifyBlockMessage returns nil if blockHashPayload contains the ID
// of an accepted block indicating it should be signed by the VM.
func (v *Verifier) verifyBlockMessage(ctx context.Context, blockHashPayload *payload.Hash) *common.AppError {
	blockID := blockHashPayload.Hash
	if err := v.blockClient.GetBlock(ctx, blockID); err != nil {
		v.blockValidationFail.Inc(1)
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("failed to get block %s: %s", blockID, err),
		}
	}

	return nil
}

// verifyOffchainAddressedCall verifies the addressed call message
func (v *Verifier) verifyOffchainAddressedCall(addressedCall *payload.AddressedCall) *common.AppError {
	if len(addressedCall.SourceAddress) != 0 {
		v.addressedCallValidationFail.Inc(1)
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: "source address should be empty for offchain addressed messages",
		}
	}

	uptimeMsg, err := message.ParseValidatorUptime(addressedCall.Payload)
	if err != nil {
		v.messageParseFail.Inc(1)
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("failed to parse addressed call message: %s", err),
		}
	}

	if err := v.verifyUptimeMessage(uptimeMsg); err != nil {
		v.uptimeValidationFail.Inc(1)
		return err
	}

	return nil
}

func (v *Verifier) verifyUptimeMessage(uptimeMsg *message.ValidatorUptime) *common.AppError {
	currentUptime, _, err := v.uptimeTracker.GetUptime(uptimeMsg.ValidationID)
	if err != nil {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("failed to get uptime: %s", err),
		}
	}

	currentUptimeSeconds := uint64(currentUptime.Seconds())
	// verify the current uptime against the total uptime in the message
	if currentUptimeSeconds < uptimeMsg.TotalUptime {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("current uptime %d is less than queried uptime %d for validationID %s", currentUptimeSeconds, uptimeMsg.TotalUptime, uptimeMsg.ValidationID),
		}
	}

	return nil
}
