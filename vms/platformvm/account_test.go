// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestAccountVerifyNoID(t *testing.T) {
	account := Account{
		Address: ids.ShortID{},
		Nonce:   defaultNonce,
		Balance: defaultBalance,
	}

	if err := account.Verify(); err == nil {
		t.Fatal("should've failed because ID is empty")
	}
}

func TestAccountRemoveMaxNonce(t *testing.T) {
	account := Account{
		Address: defaultKey.PublicKey().Address(),
		Nonce:   math.MaxUint64,
		Balance: defaultBalance,
	}

	_, err := account.Remove(defaultBalance-txFee, account.Nonce)
	if err == nil {
		t.Fatal("should have failed because account is out of nonces")
	}
}

func TestAccountRemoveWrongNonce(t *testing.T) {
	account := Account{
		Address: defaultKey.PublicKey().Address(),
		Nonce:   defaultNonce,
		Balance: defaultBalance,
	}

	_, err := account.Remove(defaultBalance-txFee, account.Nonce)
	if err == nil {
		t.Fatal("should have failed because nonce in argument is wrong")
	}
}

func TestAccountRemoveLockFunds(t *testing.T) {
	account := Account{
		Address: defaultKey.PublicKey().Address(),
		Nonce:   math.MaxUint64 - 1,
		Balance: defaultBalance,
	}

	_, err := account.Remove(defaultBalance-txFee-1, account.Nonce+1)
	if err == nil {
		t.Fatal("should have failed because funds would be locked")
	}
}

func TestAccountRemoveInvalid(t *testing.T) {
	account := Account{
		Address: ids.ShortID{},
		Nonce:   defaultNonce,
		Balance: defaultBalance,
	}

	_, err := account.Remove(defaultBalance-txFee, account.Nonce+1)
	if err == nil {
		t.Fatal("should have failed because account is invalid (ID is empty)")
	}
}

func TestRemoveOverflow(t *testing.T) {
	// this test is only meaningful if txFee is non-zero
	if txFee == 0 {
		return
	}
	account := Account{
		Address: defaultKey.PublicKey().Address(),
		Nonce:   defaultNonce,
		Balance: math.MaxUint64,
	}

	_, err := account.Remove(account.Balance, account.Nonce+1)
	if err == nil {
		t.Fatal("should have failed because amount to remove plus tx fee overflows")
	}
}

// Remove all funds
func TestRemoveAllFunds(t *testing.T) {
	account := Account{
		Address: defaultKey.PublicKey().Address(),
		Nonce:   defaultNonce,
		Balance: defaultBalance,
	}

	account, err := account.Remove(defaultBalance-txFee, account.Nonce+1)
	if err != nil {
		t.Fatal(err)
	}

	if account.Balance != 0 {
		t.Fatal("account balance should be 0")
	}
	if account.Nonce != defaultNonce+1 {
		t.Fatal("nonce should've been inccremented")
	}
	if !account.Address.Equals(defaultKey.PublicKey().Address()) {
		t.Fatal("Address shouldn't have changed")
	}
}

func TestAccountAddInvalid(t *testing.T) {
	account := Account{
		Address: ids.ShortID{},
		Nonce:   defaultNonce,
		Balance: defaultBalance,
	}

	if _, err := account.Add(1); err == nil {
		t.Fatal("should have error because account is invalid (has empty ID)")
	}
}

func TestAccountAddMaxNonce(t *testing.T) {
	account := Account{
		Address: defaultKey.PublicKey().Address(),
		Nonce:   math.MaxUint64,
		Balance: defaultBalance,
	}

	if _, err := account.Add(1); err == nil {
		t.Fatal("should have errored because account is out of nonces")
	}
}

func TestAccountAddValid(t *testing.T) {
	account := Account{
		Address: defaultKey.PublicKey().Address(),
		Nonce:   defaultNonce,
		Balance: defaultBalance,
	}

	if _, err := account.Add(1); err != nil {
		t.Fatal(err)
	}
}

func TestMarshalAccount(t *testing.T) {
	account := newAccount(
		defaultKey.PublicKey().Address(),
		defaultNonce,
		defaultBalance,
	)

	bytes, err := Codec.Marshal(account)
	if err != nil {
		t.Fatal(err)
	}

	accountUnmarshaled := &Account{}
	err = Codec.Unmarshal(bytes, accountUnmarshaled)
	if err != nil {
		t.Fatal(err)
	}

	if !account.Address.Equals(accountUnmarshaled.Address) {
		t.Fatal("IDs should match")
	}
	if account.Balance != accountUnmarshaled.Balance {
		t.Fatal("Balances should match")
	}
	if account.Nonce != accountUnmarshaled.Nonce {
		t.Fatal("Nonces don't match")
	}
}
