// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	evmwarp "github.com/ava-labs/avalanchego/vms/evm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/prometheus/client_golang/prometheus"
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

var codecManager codec.Manager

func init() {
	codecManager = codec.NewManager(maxMessageSize)
	lc := linearcodec.NewDefault()

	err := errors.Join(
		lc.RegisterType(&ValidatorUptime{}),
		codecManager.RegisterCodec(codecVersion, lc),
	)
	if err != nil {
		panic(err)
	}
}

// ValidatorUptime is signed when the ValidationID is known and the validator
// has been up for TotalUptime seconds.
type ValidatorUptime struct {
	ValidationID       ids.ID `serialize:"true"`
	TotalUptimeSeconds uint64 `serialize:"true"`
}

// ParseValidatorUptime converts a slice of bytes into a ValidatorUptime.
func ParseValidatorUptime(b []byte) (*ValidatorUptime, error) {
	var msg ValidatorUptime
	if _, err := codecManager.Unmarshal(b, &msg); err != nil {
		return nil, fmt.Errorf("parsing validator uptime: %w", err)
	}
	return &msg, nil
}

func (v *ValidatorUptime) Bytes() ([]byte, error) {
	return codecManager.Marshal(codecVersion, v)
}


type Verifier struct {
	uptimeTracker *uptimetracker.UptimeTracker

	unknownVerifier evmwarp.NoVerifier
	addressedCallVerifyFail prometheus.Counter
	uptimeVerifyFail        prometheus.Counter
}

var _ evmwarp.VerifyHandler = (*Verifier)(nil)

func NewVerifier(
	ut *uptimetracker.UptimeTracker,
	r prometheus.Registerer,
) (*Verifier, error) {
	v := &Verifier{
		uptimeTracker: ut,
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

func (v *Verifier) Verify(
	p payload.Payload,
	messageParseFail prometheus.Counter,
) error {
	addressedCall, ok := p.(*payload.AddressedCall)
	if !ok {
		return v.unknownVerifier.Verify(p, messageParseFail)
	}

	if len(addressedCall.SourceAddress) != 0 {
		v.addressedCallVerifyFail.Inc()
		return &common.AppError{
			Message: "source address should be empty for offchain addressed messages",
		}
	}

	uptimeMsg, err := ParseValidatorUptime(addressedCall.Payload)
	if err != nil {
		messageParseFail.Inc()
		return &common.AppError{
			Message: fmt.Sprintf("failed to parse addressed call message: %s", err),
		}
	}

	currentUptime, _, err := v.uptimeTracker.GetUptime(uptimeMsg.ValidationID)
	if err != nil {
		v.uptimeVerifyFail.Inc()
		return &common.AppError{
			Message: fmt.Sprintf("failed to get uptime: %s", err),
		}
	}

	currentUptimeSeconds := uint64(currentUptime.Seconds())
	// verify the current uptime against the total uptime in the message
	if currentUptimeSeconds < uptimeMsg.TotalUptimeSeconds {
		v.uptimeVerifyFail.Inc()
		return &common.AppError{
			Message: fmt.Sprintf("current uptime %d is less than queried uptime %d for validationID %s", currentUptimeSeconds, uptimeMsg.TotalUptimeSeconds, uptimeMsg.ValidationID),
		}
	}

	return nil
}
