// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"
)

func TestInputVerifyNil(t *testing.T) {
	in := (*Input)(nil)
	if err := in.Verify(); err == nil {
		t.Fatalf("Input.Verify should have returned an error due to an nil input")
	}
}
