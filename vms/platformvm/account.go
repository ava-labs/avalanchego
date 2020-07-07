// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	stdmath "math"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/utils/units"
)

var (
	txFee = uint64(0) * units.MicroAva // The transaction fee
)

var (
	errOutOfSpends = errors.New("ran out of spends")
	errInvalidID   = errors.New("invalid ID")
)

// Account represents the Balance and nonce of a user's funds
type Account struct {
	// Address of this account
	// Its value is [privKey].PublicKey().Address() where privKey
	// is the private key that controls this account
	Address ids.ShortID `serialize:"true"`

	// Nonce this account was last spent with
	// Initially, this is set to 0. Therefore, the first nonce a transaction should
	// use for a new account is 1.
	Nonce uint64 `serialize:"true"`

	// Balance of $AVA held by this account
	Balance uint64 `serialize:"true"`
}

// Remove generates a new account state from removing [amount + txFee] from [a]'s balance.
// [nonce] is [a]'s next unused nonce
func (a Account) Remove(amount, nonce uint64) (Account, error) {
	// Ensure account is in a valid state
	if err := a.Verify(); err != nil {
		return a, err
	}

	// Ensure account's nonce isn't used up.
	// For this error to occur, an account would need to be issuing transactions
	// at 10k tps for ~ 80 million years
	newNonce, err := math.Add64(a.Nonce, 1)
	if err != nil {
		return a, errOutOfSpends
	}

	if newNonce != nonce {
		return a, fmt.Errorf("account's last nonce is %d so expected tx nonce to be %d but was %d", a.Nonce, newNonce, nonce)
	}

	amountWithFee, err := math.Add64(amount, txFee)
	if err != nil {
		return a, fmt.Errorf("send amount overflowed: tx fee (%d) + send amount (%d) > maximum value", txFee, amount)
	}

	newBalance, err := math.Sub64(a.Balance, amountWithFee)
	if err != nil {
		return a, fmt.Errorf("insufficient funds: account balance %d < tx fee (%d) + send amount (%d)", a.Balance, txFee, amount)
	}

	// Ensure this tx wouldn't lock funds
	if newNonce == stdmath.MaxUint64 && newBalance != 0 {
		return a, fmt.Errorf("transaction would lock %d funds", newBalance)
	}

	return Account{
		Address: a.Address,
		Nonce:   newNonce,
		Balance: newBalance,
	}, nil
}

// Add returns the state of [a] after receiving the $AVA
func (a Account) Add(amount uint64) (Account, error) {
	// Ensure account is in a valid state
	if err := a.Verify(); err != nil {
		return a, err
	}

	// Ensure account's nonce isn't used up
	// For this error to occur, a user would need to be issuing transactions
	// at 10k tps for ~ 80 million years
	if a.Nonce == stdmath.MaxUint64 {
		return a, errOutOfSpends
	}

	// account's balance after receipt of staked $AVA
	newBalance, err := math.Add64(a.Balance, amount)
	if err != nil {
		return a, fmt.Errorf("account balance (%d) + staked $AVA (%d) exceeds maximum uint64", a.Balance, amount)
	}

	return Account{
		Address: a.Address,
		Nonce:   a.Nonce,
		Balance: newBalance,
	}, nil
}

// Verify that this account is in a valid state
func (a Account) Verify() error {
	switch {
	case a.Address.IsZero():
		return errInvalidID
	default:
		return nil
	}
}

// Bytes returns the byte representation of this account
func (a Account) Bytes() []byte {
	bytes, _ := Codec.Marshal(a)
	return bytes
}

func newAccount(Address ids.ShortID, Nonce, Balance uint64) Account {
	return Account{
		Address: Address,
		Nonce:   Nonce,
		Balance: Balance,
	}
}
