// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"
)

func TestMetaDataVerifyNil(t *testing.T) {
	md := (*metadata)(nil)
	if err := md.Verify(); err == nil {
		t.Fatalf("Should have errored due to nil metadata")
	}
}

func TestMetaDataVerifyUninitialized(t *testing.T) {
	md := &metadata{}
	if err := md.Verify(); err == nil {
		t.Fatalf("Should have errored due to uninitialized metadata")
	}
}
