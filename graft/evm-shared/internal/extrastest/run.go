// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extrastest

import (
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/libevm"
)

type TestState int

const (
	Coreth TestState = iota
	SubnetEVM
)

type TempRegisterer func(libevm.ExtrasLock, func() error) error

func RunWith(m *testing.M, registrations ...TempRegisterer) error {
	inner := func() error {
		code := m.Run()
		if code != 0 {
			return fmt.Errorf("tests failed with code %d", code)
		}
		return nil
	}

	if err := libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
		for _, wrap := range registrations {
			innerFn := inner
			inner = func() error { return wrap(lock, innerFn) }
		}
		return inner()
	}); err != nil {
		return err
	}
	return nil
}
