// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"errors"
	"testing"
)

var errRegister = errors.New("unexpectedly called Register")

type TestStateSyncableBlock struct {
	TestBlock

	T            *testing.T
	CantRegister bool
	RegisterF    func() error
}

func (s *TestStateSyncableBlock) Register() error {
	if s.RegisterF != nil {
		return s.RegisterF()
	}
	if s.CantRegister && s.T != nil {
		s.T.Fatalf("Unexpectedly called Accept")
	}
	return errRegister
}
