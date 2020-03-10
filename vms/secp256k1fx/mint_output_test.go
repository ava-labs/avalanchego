// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"
)

func TestMintOutputVerifyNil(t *testing.T) {
	out := (*MintOutput)(nil)
	if err := out.Verify(); err == nil {
		t.Fatalf("MintOutput.Verify should have returned an error due to an nil output")
	}
}
