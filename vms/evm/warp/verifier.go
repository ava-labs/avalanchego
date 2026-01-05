// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/libevm/metrics"
)

const (
	ParseErrCode = iota + 1
	VerifyErrCode

	codecVersion   = 0
	maxMessageSize = 24 * units.KiB
)

var (
	_ acp118.Verifier = (*acp118Handler)(nil)

	c codec.Manager
)

func init() {
	c = codec.NewManager(maxMessageSize)
	lc := linearcodec.NewDefault()

	err := errors.Join(
		lc.RegisterType(&ValidatorUptime{}),
		c.RegisterCodec(codecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}

// ValidatorUptime is signed when the ValidationID is known and the validator
// has been up for TotalUptime seconds.
type ValidatorUptime struct {
	ValidationID ids.ID `serialize:"true"`
	TotalUptime  uint64 `serialize:"true"` // in seconds
}

// ParseValidatorUptime converts a slice of bytes into a ValidatorUptime.
func ParseValidatorUptime(b []byte) (*ValidatorUptime, error) {
	var msg ValidatorUptime
	if _, err := c.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// Bytes returns the binary representation of this payload.
func (v *ValidatorUptime) Bytes() ([]byte, error) {
	return c.Marshal(codecVersion, v)
}

// BlockStore provides access to accepted blocks.
type BlockStore interface {
	HasBlock(ctx context.Context, blockID ids.ID) error
}

// Verifier validates whether a warp message should be signed.
type Verifier struct {
	db            *DB
	blockClient   BlockStore
	uptimeTracker *uptimetracker.UptimeTracker

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

// Verify validates whether a warp message should be signed.
func (v *Verifier) Verify(ctx context.Context, unsignedMessage *warp.UnsignedMessage) *common.AppError {
	messageID := unsignedMessage.ID()
	// Known on-chain messages should be signed
	if _, err := v.db.Get(messageID); err == nil {
		return nil
	} else if !errors.Is(err, database.ErrNotFound) {
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
	if err := v.blockClient.HasBlock(ctx, blockID); err != nil {
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

	uptimeMsg, err := ParseValidatorUptime(addressedCall.Payload)
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

func (v *Verifier) verifyUptimeMessage(uptimeMsg *ValidatorUptime) *common.AppError {
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

// acp118Handler supports signing warp messages requested by peers.
type acp118Handler struct {
	verifier *Verifier
}

func (a *acp118Handler) Verify(ctx context.Context, message *warp.UnsignedMessage, _ []byte) *common.AppError {
	return a.verifier.Verify(ctx, message)
}

// NewHandler returns a handler for signing warp messages requested by peers.
func NewHandler(
	signatureCache cache.Cacher[ids.ID, []byte],
	verifier *Verifier,
	signer warp.Signer,
) *acp118.Handler {
	return acp118.NewCachedHandler(
		signatureCache,
		&acp118Handler{verifier: verifier},
		signer,
	)
}
