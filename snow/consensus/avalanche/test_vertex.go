// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/choices"
	"github.com/chain4travel/caminogo/snow/consensus/snowstorm"
)

var _ Vertex = &TestVertex{}

// TestVertex is a useful test vertex
type TestVertex struct {
	choices.TestDecidable

	VerifyErrV    error
	ParentsV      []Vertex
	ParentsErrV   error
	HasWhitelistV bool
	WhitelistV    ids.Set
	WhitelistErrV error
	HeightV       uint64
	HeightErrV    error
	TxsV          []snowstorm.Tx
	TxsErrV       error
	BytesV        []byte
}

func (v *TestVertex) Verify() error                { return v.VerifyErrV }
func (v *TestVertex) Parents() ([]Vertex, error)   { return v.ParentsV, v.ParentsErrV }
func (v *TestVertex) HasWhitelist() bool           { return v.HasWhitelistV }
func (v *TestVertex) Whitelist() (ids.Set, error)  { return v.WhitelistV, v.WhitelistErrV }
func (v *TestVertex) Height() (uint64, error)      { return v.HeightV, v.HeightErrV }
func (v *TestVertex) Txs() ([]snowstorm.Tx, error) { return v.TxsV, v.TxsErrV }
func (v *TestVertex) Bytes() []byte                { return v.BytesV }
