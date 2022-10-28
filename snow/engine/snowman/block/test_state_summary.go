// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ StateSummary = (*TestStateSummary)(nil)

	errAccept = errors.New("unexpectedly called Accept")
)

type TestStateSummary struct {
	IDV     ids.ID
	HeightV uint64
	BytesV  []byte

	T          *testing.T
	CantAccept bool
	AcceptF    func() (bool, error)
}

func (s *TestStateSummary) ID() ids.ID     { return s.IDV }
func (s *TestStateSummary) Height() uint64 { return s.HeightV }
func (s *TestStateSummary) Bytes() []byte  { return s.BytesV }

func (s *TestStateSummary) Accept() (bool, error) {
	if s.AcceptF != nil {
		return s.AcceptF()
	}
	if s.CantAccept && s.T != nil {
		s.T.Fatal(errAccept)
	}
	return false, errAccept
}
