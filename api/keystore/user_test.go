// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"testing"
)

func TestUser(t *testing.T) {
	usr := User{}
	if err := usr.Initialize("heytherepal"); err != nil {
		t.Fatal(err)
	}
	if !usr.CheckPassword("heytherepal") {
		t.Fatalf("Should have verified the password")
	}
	if usr.CheckPassword("heytherepal!") {
		t.Fatalf("Shouldn't have verified the password")
	}
	if usr.CheckPassword("") {
		t.Fatalf("Shouldn't have verified the password")
	}
}
