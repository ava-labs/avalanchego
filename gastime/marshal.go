// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"errors"
	"fmt"

	"github.com/StephenButtolph/canoto"
	"github.com/ava-labs/avalanchego/vms/components/gas"

	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/proxytime"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

type config struct {
	targetToExcessScaling gas.Gas   `canoto:"uint,1"`
	minPrice              gas.Price `canoto:"uint,2"`
	staticPricing         bool      `canoto:"bool,3"`

	canotoData canotoData_config
}

var errInvalidGasPriceConfig = errors.New("invalid gas price config")

// newConfig builds and validates an internal config from [hook.GasPriceConfig].
func newConfig(from hook.GasPriceConfig) (config, error) {
	if err := from.Validate(); err != nil {
		return config{}, fmt.Errorf("%w: %w", errInvalidGasPriceConfig, err)
	}
	c := config{
		targetToExcessScaling: from.TargetToExcessScaling,
		minPrice:              from.MinPrice,
		staticPricing:         from.StaticPricing,
	}
	return c, nil
}

// equal returns true if the logical fields of c and other are equal.
// It ignores canoto internal fields.
func (c config) equals(other config) bool {
	return c.targetToExcessScaling == other.targetToExcessScaling &&
		c.minPrice == other.minPrice &&
		c.staticPricing == other.staticPricing
}

// A TimeMarshaler can marshal a time to and from canoto. It is of limited use
// by itself and MUST only be used via a wrapping [Time].
type TimeMarshaler struct { //nolint:tagliatelle // TODO(arr4n) submit linter bug report
	*proxytime.Time[gas.Gas] `canoto:"pointer,1"`
	target                   gas.Gas `canoto:"uint,2"`
	excess                   gas.Gas `canoto:"uint,3"`
	config                   config  `canoto:"value,4"`

	// The nocopy is important, not only for canoto, but because of the use of
	// pointers in [Time.establishInvariants]. See [Time.Clone].
	canotoData canotoData_TimeMarshaler `canoto:"nocopy"`
}

var _ canoto.Message = (*Time)(nil)

// MakeCanoto creates a new empty value.
func (*Time) MakeCanoto() *Time { return new(Time) }

// UnmarshalCanoto unmarshals the bytes into the [TimeMarshaler] and then
// reestablishes invariants.
func (tm *Time) UnmarshalCanoto(bytes []byte) error {
	r := canoto.Reader{
		B: bytes,
	}
	return tm.UnmarshalCanotoFrom(r)
}

// UnmarshalCanotoFrom populates the [TimeMarshaler] from the reader and then
// reestablishes invariants.
func (tm *Time) UnmarshalCanotoFrom(r canoto.Reader) error {
	if err := tm.TimeMarshaler.UnmarshalCanotoFrom(r); err != nil {
		return err
	}
	tm.establishInvariants()
	return nil
}
