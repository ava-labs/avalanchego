// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	evmwarp "github.com/ava-labs/avalanchego/vms/evm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/prometheus/client_golang/prometheus"
	"context"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"fmt"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"errors"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	codecVersion   = 0
	maxMessageSize = 24 * units.KiB
)

var c codec.Manager

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
	ValidationID       ids.ID `serialize:"true"`
	TotalUptimeSeconds uint64 `serialize:"true"` // in seconds
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


var _ evmwarp.VerifyHandler = (*Verifier)(nil)

type Verifier struct {
	uptimeTracker *uptimetracker.UptimeTracker

	blockVerifier *evmwarp.BlockVerifier
	addressedCallVerifyFail prometheus.Counter
	uptimeVerifyFail        prometheus.Counter
}

func NewVerifier(
	vm evmwarp.VM,
	ut *uptimetracker.UptimeTracker,
	r prometheus.Registerer,
) (*Verifier, error) {
	blockVerifier, err := evmwarp.NewBlockVerifier(vm, r)
	if err != nil {
		return nil, err
	}

	v := &Verifier{
		uptimeTracker: ut,
		blockVerifier: blockVerifier,
		addressedCallVerifyFail: prometheus.NewCounter(prometheus.CounterOpts{
		Name: "warp_backend_addressed_call_verify_fail",
		Help: "Number of addressed call verification failures",
	}),
		uptimeVerifyFail: prometheus.NewCounter(prometheus.CounterOpts{
		Name: "warp_backend_uptime_verify_fail",
		Help: "Number of uptime verification failures",
	}),
	}

	if err := r.Register(v.addressedCallVerifyFail); err != nil {
		return nil, err
	}

	if err := r.Register(v.uptimeVerifyFail); err != nil {
		return nil, err
	}

	return v, nil
}

func (v *Verifier) VerifyBlockHash(
	ctx context.Context,
	p *payload.Hash,
) *common.AppError {
	return v.blockVerifier.VerifyBlockHash(ctx, p)
}

func (v *Verifier) VerifyUnknown(
	p payload.Payload,
	messageParseFail prometheus.Counter,
) *common.AppError {
	ac, ok :=  p.(*payload.AddressedCall)
	if !ok {
		return v.blockVerifier.VerifyUnknown(p, messageParseFail)
	}

	return v.verifyOffchainAddressedCall(ac, messageParseFail)
}

func (v *Verifier) verifyOffchainAddressedCall(
	addressedCall *payload.AddressedCall,
	messageParseFail prometheus.Counter,
) *common.AppError {
	if len(addressedCall.SourceAddress) != 0 {
		v.addressedCallVerifyFail.Inc()
		return &common.AppError{
			Code:    evmwarp.VerifyErrCode,
			Message: "source address should be empty for offchain addressed messages",
		}
	}

	uptimeMsg, err := ParseValidatorUptime(addressedCall.Payload)
	if err != nil {
		messageParseFail.Inc()
		return &common.AppError{
			Code:    evmwarp.ParseErrCode,
			Message: fmt.Sprintf("failed to parse addressed call message: %s", err),
		}
	}

	if err := v.verifyUptimeMessage(uptimeMsg); err != nil {
		v.uptimeVerifyFail.Inc()
		return err
	}

	return nil
}

func (v *Verifier) verifyUptimeMessage(uptimeMsg *ValidatorUptime) *common.AppError {
	currentUptime, _, err := v.uptimeTracker.GetUptime(uptimeMsg.ValidationID)
	if err != nil {
		return &common.AppError{
			Code:    evmwarp.VerifyErrCode,
			Message: fmt.Sprintf("failed to get uptime: %s", err),
		}
	}

	currentUptimeSeconds := uint64(currentUptime.Seconds())
	// verify the current uptime against the total uptime in the message
	if currentUptimeSeconds < uptimeMsg.TotalUptimeSeconds {
		return &common.AppError{
			Code:    evmwarp.VerifyErrCode,
			Message: fmt.Sprintf("current uptime %d is less than queried uptime %d for validationID %s", currentUptimeSeconds, uptimeMsg.TotalUptimeSeconds, uptimeMsg.ValidationID),
		}
	}

	return nil
}
