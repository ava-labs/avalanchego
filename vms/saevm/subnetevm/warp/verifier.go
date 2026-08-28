// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/messages"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
)

// Error codes returned for subnet-evm-specific verification outcomes. They
// share the [saewarp] code space: parse failures reuse
// [saewarp.ParseErrCode] and verification failures share the value of
// [saewarp.NotAcceptedErrCode].
const (
	ParseErrCode  = saewarp.ParseErrCode
	VerifyErrCode = saewarp.NotAcceptedErrCode
)

// UptimeSource is the slice of `*uptimetracker.UptimeTracker` consumed
// by the warp [UptimeVerifier] for `*messages.ValidatorUptime` messages.
// Pulled out as a one-method interface so the verifier does not bind
// to the concrete tracker type and so tests can inject stubs without
// constructing a full tracker. The exported signature matches
// `*uptimetracker.UptimeTracker.GetUptime` exactly: the concrete
// tracker satisfies it without an adapter.
type UptimeSource interface {
	GetUptime(validationID ids.ID) (time.Duration, time.Time, error)
}

// NewVerifier returns an ACP-118 message verifier: the shared [saewarp]
// verifier extended with subnet-evm's validator-uptime attestation handling.
// Pass `nil` for `uptime` if the chain does not yet wire up uptime
// accounting; uptime messages are then refused with [VerifyErrCode].
func NewVerifier(blocks saewarp.Backend, storage *saewarp.Storage, uptime UptimeSource) *saewarp.Verifier {
	return saewarp.NewVerifier(blocks, storage, &UptimeVerifier{uptime: uptime})
}

var _ saewarp.AddressedCallVerifier = (*UptimeVerifier)(nil)

// An UptimeVerifier decides whether this node should sign
// `*payload.AddressedCall` messages carrying a known [messages.Payload]. The
// only currently-handled type is `*messages.ValidatorUptime`: signed iff the
// source address is empty and the locally-tracked uptime for the validation
// ID is at least the message's claimed `TotalUptime`.
type UptimeVerifier struct {
	uptime UptimeSource
}

// VerifyAddressedCall verifies a `*payload.AddressedCall`.
//
//   - The source address MUST be empty for addressed messages.
//   - The payload MUST parse as a known `messages.Payload` type. The
//     only currently-handled type is `*messages.ValidatorUptime`.
func (v *UptimeVerifier) VerifyAddressedCall(addressedCall *payload.AddressedCall) *common.AppError {
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
func (v *UptimeVerifier) verifyUptime(uptimeMsg *messages.ValidatorUptime) *common.AppError {
	if v.uptime == nil {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: "no uptime source configured",
		}
	}
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
