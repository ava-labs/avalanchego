// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Summary = &TestSummary{}

	errAccept = errors.New("unexpectedly called Accept")
)

type TestSummary struct {
	HeightV uint64
	IDV     ids.ID
	BytesV  []byte

	T          *testing.T
	CantAccept bool
	AcceptF    func() (bool, error)
}

func (s *TestSummary) Bytes() []byte  { return s.BytesV }
func (s *TestSummary) Height() uint64 { return s.HeightV }
func (s *TestSummary) ID() ids.ID     { return s.IDV }

func (s *TestSummary) Accept() (bool, error) {
	if s.AcceptF != nil {
		return s.AcceptF()
	}
	if s.CantAccept && s.T != nil {
		s.T.Fatalf("Unexpectedly called Accept")
	}
	return false, errAccept
}
