// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package unwind_test

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/saevm/unwind"
)

func stagedConstructor(fail bool) (retErr error) {
	var closers unwind.Closers
	defer closers.CloseIfPointsToNonNil(&retErr)

	for _, s := range []string{"A", "B", "C"} {
		closers.Push(unwind.CloserFunc(func() error {
			return errors.New(s)
		}))
	}

	if fail {
		return errors.New("primary error")
	}
	return nil
}

func ExampleClosers_CloseIfPointsToNonNil() {
	fmt.Println("Non-failing:", stagedConstructor(false))
	fmt.Println("Failing:", stagedConstructor(true))

	// Output:
	// Non-failing: <nil>
	// Failing: primary error
	// C
	// B
	// A
}
