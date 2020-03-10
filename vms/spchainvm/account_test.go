// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

// TODO: Add package comment describing what spvm (simple payements vm means)

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestAccountSerialization(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	builder := Builder{
		NetworkID: 0,
		ChainID:   chainID,
	}
	account := builder.NewAccount(ids.ShortEmpty, 5, 25)

	codec := Codec{}
	bytes, err := codec.MarshalAccount(account)
	if err != nil {
		t.Fatal(err)
	}

	newAccount, err := codec.UnmarshalAccount(bytes)
	if err != nil {
		t.Fatal(err)
	}

	if account.String() != newAccount.String() {
		t.Fatalf("Expected %s got %s", account, newAccount)
	}
}
