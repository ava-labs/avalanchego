// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"
)

func TestFactory(t *testing.T) {
	factory := Factory{}
	if fx := factory.New(); fx == nil {
		t.Fatalf("Factory.New returned nil")
	}
}
