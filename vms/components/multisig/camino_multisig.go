// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package multisig

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/types"
)

// MaxMemoSize is the maximum number of bytes in the memo field
const MaxMemoSize = 256

var (
	_ verify.State = (*Alias)(nil)

	errMemoIsTooBig = errors.New("msig alias memo is too big")
	errEmptyAlias   = errors.New("alias id and alias owners cannot be empty both at the same time")
)

type Owners interface {
	verify.State

	IsZero() bool
}

type Alias struct {
	ID     ids.ShortID         `serialize:"true" json:"id"`
	Memo   types.JSONByteSlice `serialize:"true" json:"memo"`
	Owners Owners              `serialize:"true" json:"owners"`
}

type AliasWithNonce struct {
	Alias `serialize:"true" json:"alias"`

	// Nonce reflects how many times the owners of this alias have changed
	Nonce uint64 `serialize:"true" json:"nonce"`
}

func (ma *Alias) InitCtx(ctx *snow.Context) {
	ma.Owners.InitCtx(ctx)
}

func (ma *Alias) Verify() error {
	switch {
	case len(ma.Memo) > MaxMemoSize:
		return fmt.Errorf("%w: expected not greater than %d bytes, got %d bytes", errMemoIsTooBig, MaxMemoSize, len(ma.Memo))
	case ma.Owners.IsZero() && ma.ID == ids.ShortEmpty:
		return errEmptyAlias
	}

	return ma.Owners.Verify()
}

func (ma *Alias) VerifyState() error {
	return ma.Verify()
}

func ComputeAliasID(txID ids.ID) ids.ShortID {
	return hashing.ComputeHash160Array(txID[:])
}
