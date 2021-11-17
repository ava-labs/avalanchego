// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestOwnedOutputState(t *testing.T) {
	intf := interface{}(&OwnedOutput{})
	if _, ok := intf.(verify.State); !ok {
		t.Fatalf("should be marked as state")
	}
}
