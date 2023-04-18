// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestFactory(t *testing.T) {
	factory := Factory{}
	if fx, err := factory.New(logging.NoLog{}); err != nil {
		t.Fatal(err)
	} else if fx == nil {
		t.Fatalf("Factory.New returned nil")
	}
}
