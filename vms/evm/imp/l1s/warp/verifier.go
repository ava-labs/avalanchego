// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warp extends [warp] message verification with validator uptime
// attestations.
package warp

import (
	"fmt"

	"github.com/ava-labs/avalanchego/graft/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/messages"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

// The error codes are returned by the [warp.Verifier] from [NewVerifier] to
// identify why a validator uptime attestation was not signed.
const (
	SourceAddressErrCode = warp.NextErrCode + iota
	ParseMessageErrCode
	UnknownMessageTypeErrCode
	GetUptimeErrCode
	InsufficientUptimeErrCode
)

// NewVerifier returns a [warp.Verifier] that also accepts a
// [messages.ValidatorUptime] claiming no more uptime than `uptime` tracked.
//
// The attestation's source address MUST be empty. A contract's message is
// signable only once it is in [warp.Storage], which the [warp.Verifier] checks
// before verifying attestations.
func NewVerifier(
	blocks warp.Backend,
	storage *warp.Storage,
	uptime *uptimetracker.UptimeTracker,
) *warp.Verifier {
	return warp.NewVerifier(
		blocks,
		storage,
		warp.WithAddressedCallVerifier(&uptimeVerifier{uptime: uptime}),
	)
}

var _ warp.AddressedCallVerifier = (*uptimeVerifier)(nil)

// An uptimeVerifier verifies validator uptime attestations.
type uptimeVerifier struct {
	uptime *uptimetracker.UptimeTracker
}

func (v *uptimeVerifier) VerifyAddressedCall(addressedCall *payload.AddressedCall) *common.AppError {
	if len(addressedCall.SourceAddress) != 0 {
		return &common.AppError{
			Code:    SourceAddressErrCode,
			Message: "source address should be empty for addressed messages",
		}
	}

	parsed, err := messages.Parse(addressedCall.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ParseMessageErrCode,
			Message: "failed to parse addressed call message: " + err.Error(),
		}
	}

	switch p := parsed.(type) {
	case *messages.ValidatorUptime:
		return v.verifyUptime(p)
	default:
		return &common.AppError{
			Code:    UnknownMessageTypeErrCode,
			Message: fmt.Sprintf("unknown message type: %T", p),
		}
	}
}

func (v *uptimeVerifier) verifyUptime(msg *messages.ValidatorUptime) *common.AppError {
	duration, _, err := v.uptime.GetUptime(msg.ValidationID)
	if err != nil {
		return &common.AppError{
			Code:    GetUptimeErrCode,
			Message: "failed to get uptime: " + err.Error(),
		}
	}

	seconds := uint64(duration.Seconds()) //#nosec G115 -- uptime durations are non-negative and bounded by chain age
	if seconds < msg.TotalUptime {
		return &common.AppError{
			Code: InsufficientUptimeErrCode,
			Message: fmt.Sprintf(
				"current uptime %d is less than queried uptime %d for validationID %s",
				seconds, msg.TotalUptime, msg.ValidationID,
			),
		}
	}
	return nil
}
