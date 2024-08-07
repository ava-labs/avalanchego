// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package multisig

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/types"
)

// MaxMemoSize is the maximum number of bytes in the memo field
const MaxMemoSize = 256

type Alias struct {
	ID     ids.ShortID         `serialize:"true" json:"id"`
	Memo   types.JSONByteSlice `serialize:"true" json:"memo"`
	Owners verify.State        `serialize:"true" json:"owners"`
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
	if len(ma.Memo) > MaxMemoSize {
		return fmt.Errorf("msig alias memo is larger (%d bytes) than max of %d bytes", len(ma.Memo), MaxMemoSize)
	}

	return ma.Owners.Verify()
}

func (ma *Alias) VerifyState() error {
	return ma.Verify()
}

func ComputeAliasID(txID ids.ID) ids.ShortID {
	return hashing.ComputeHash160Array(txID[:])
}
