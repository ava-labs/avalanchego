// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"
)

func TestCredentialVerifyNil(t *testing.T) {
	cred := (*Credential)(nil)
	if err := cred.Verify(); err == nil {
		t.Fatalf("Should have errored due to nil credential")
	}
}

func TestCredentialVerifyNilFx(t *testing.T) {
	cred := &Credential{}
	if err := cred.Verify(); err == nil {
		t.Fatalf("Should have errored due to nil fx credential")
	}
}

func TestCredential(t *testing.T) {
	cred := &Credential{
		Cred: &testVerifiable{},
	}

	if err := cred.Verify(); err != nil {
		t.Fatal(err)
	}

	if cred.Credential() != cred.Cred {
		t.Fatalf("Should have returned the fx credential")
	}
}
