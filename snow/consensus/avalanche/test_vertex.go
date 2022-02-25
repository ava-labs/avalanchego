// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
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
