// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/subnetevm/warp/messages"
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

// UptimeSource is the slice of `*uptimetracker.UptimeTracker` consumed
// by the warp `Verifier` for `*messages.ValidatorUptime` messages.
// Pulled out as a one-method interface so the verifier does not bind
// to the concrete tracker type and so tests can inject stubs without
// constructing a full tracker. The exported signature matches
// `*uptimetracker.UptimeTracker.GetUptime` exactly: the concrete
// tracker satisfies it without an adapter.
type UptimeSource interface {
	GetUptime(validationID ids.ID) (time.Duration, time.Time, error)
}

// Verifier verifies whether this node should be willing to sign a warp message.
//
// Two payload types are accepted:
//
//   - `*payload.Hash`: signed iff the referenced block has been
//     accepted (`BlockClient.IsAccepted`). Mirrors
//     `graft/subnet-evm/warp::verifyBlockMessage`.
//   - `*payload.AddressedCall` carrying a known
//     `messages.ValidatorUptime`: signed iff the source address is
//     empty and the locally-tracked uptime for the validation ID is at
//     least the message's claimed `TotalUptime`. Mirrors
//     `graft/subnet-evm/warp::verifyOffchainAddressedCall` +
//     `verifyUptimeMessage`.
//
// `uptime` may be nil, in which case `*payload.AddressedCall`
// containing a `*messages.ValidatorUptime` returns `VerifyErrCode`
// (the verifier cannot prove or refute the claim without a tracker).
type Verifier struct {
	blocks  BlockClient
	storage *Storage
	uptime  UptimeSource
}

// NewVerifier returns an ACP-118 message verifier. Pass `nil` for
// `uptime` if the chain does not yet wire up uptime accounting.
func NewVerifier(blocks BlockClient, storage *Storage, uptime UptimeSource) *Verifier {
	return &Verifier{
		blocks:  blocks,
		storage: storage,
		uptime:  uptime,
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

	switch p := parsed.(type) {
	case *payload.Hash:
		return v.verifyBlock(ctx, p)
	case *payload.AddressedCall:
		return v.verifyAddressedCall(p)
	default:
		return &common.AppError{
			Code:    TypeErrCode,
			Message: fmt.Sprintf("wrong payload type: %T", p),
		}
	}
}

// verifyBlock returns nil iff the referenced block has been accepted.
func (v *Verifier) verifyBlock(ctx context.Context, p *payload.Hash) *common.AppError {
	if err := v.blocks.IsAccepted(ctx, p.Hash); err != nil {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("failed to get block %s: %s", p.Hash, err.Error()),
		}
	}
	return nil
}

// verifyAddressedCall verifies a `*payload.AddressedCall`.
//
//   - The source address MUST be empty for addressed messages.
//   - The payload MUST parse as a known `messages.Payload` type. The
//     only currently-handled type is `*messages.ValidatorUptime`.
func (v *Verifier) verifyAddressedCall(addressedCall *payload.AddressedCall) *common.AppError {
	if len(addressedCall.SourceAddress) != 0 {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: "source address should be empty for addressed messages",
		}
	}

	parsed, err := messages.Parse(addressedCall.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "failed to parse addressed call message: " + err.Error(),
		}
	}

	switch p := parsed.(type) {
	case *messages.ValidatorUptime:
		return v.verifyUptime(p)
	default:
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("unknown message type: %T", p),
		}
	}
}

// verifyUptime returns nil iff the locally-tracked uptime for
// `uptimeMsg.ValidationID` is at least `uptimeMsg.TotalUptime`
// seconds
func (v *Verifier) verifyUptime(uptimeMsg *messages.ValidatorUptime) *common.AppError {
	currentUptime, _, err := v.uptime.GetUptime(uptimeMsg.ValidationID)
	if err != nil {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: "failed to get uptime: " + err.Error(),
		}
	}

	currentUptimeSeconds := uint64(currentUptime.Seconds())
	if currentUptimeSeconds < uptimeMsg.TotalUptime {
		return &common.AppError{
			Code: VerifyErrCode,
			Message: fmt.Sprintf(
				"current uptime %d is less than queried uptime %d for validationID %s",
				currentUptimeSeconds, uptimeMsg.TotalUptime, uptimeMsg.ValidationID,
			),
		}
	}
	return nil
}
