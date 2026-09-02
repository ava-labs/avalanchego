// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warptest preserves the C-Chain test helper import path.
package warptest

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	saewarptest "github.com/ava-labs/avalanchego/vms/saevm/warp/warptest"
)

type (
	Validators = saewarptest.Validators
	Option     = saewarptest.Option
)

func WithSigners(signers ...bls.Signer) Option {
	return saewarptest.WithSigners(signers...)
}

func NewValidators(tb testing.TB, opts ...Option) *Validators {
	return saewarptest.NewValidators(tb, opts...)
}
