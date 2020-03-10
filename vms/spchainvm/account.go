// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
)

var (
	errOutOfSpends       = errors.New("ran out of spends")
	errInsufficientFunds = errors.New("insufficient funds")
	errOverflow          = errors.New("math overflowed")
	errInvalidID         = errors.New("invalid ID")
	errInvalidAddress    = errors.New("invalid address")
)

// Account represents the balance and nonce of a user's funds
type Account struct {
	id             ids.ShortID
	nonce, balance uint64
}

// ID of this account
func (a Account) ID() ids.ShortID { return a.id }

// Balance contained in this account
func (a Account) Balance() uint64 { return a.balance }

// Nonce this account was last spent with
func (a Account) Nonce() uint64 { return a.nonce }

// CreateTx creates a transaction from this account
// that sends [amount] to the address [destination]
func (a Account) CreateTx(amount uint64, destination ids.ShortID, ctx *snow.Context, key *crypto.PrivateKeySECP256K1R) (*Tx, Account, error) {
	builder := Builder{
		NetworkID: ctx.NetworkID,
		ChainID:   ctx.ChainID,
	}
	// If nonce overflows, Send will return an error
	tx, err := builder.NewTx(key, a.nonce+1, amount, destination)
	if err != nil {
		return nil, a, err
	}
	newAccount, err := a.Send(tx, ctx)
	return tx, newAccount, err
}

// Send generates a new account state from sending the transaction
func (a Account) Send(tx *Tx, ctx *snow.Context) (Account, error) {
	return a.send(tx, ctx, &crypto.FactorySECP256K1R{})
}

// send generates the new account state from sending the transaction
func (a Account) send(tx *Tx, ctx *snow.Context, factory *crypto.FactorySECP256K1R) (Account, error) {
	return Account{
		id: a.id,
		// guaranteed not to overflow due to VerifySend
		nonce: a.nonce + 1,
		// guaranteed not to underflow due to VerifySend
		balance: a.balance - tx.amount,
	}, a.verifySend(tx, ctx, factory)
}

// VerifySend returns if the provided transaction can send this transaction
func (a Account) VerifySend(tx *Tx, ctx *snow.Context) error {
	return a.verifySend(tx, ctx, &crypto.FactorySECP256K1R{})
}

func (a Account) verifySend(tx *Tx, ctx *snow.Context, factory *crypto.FactorySECP256K1R) error {
	// Verify the account is in a valid state and the transaction is valid
	if err := a.Verify(); err != nil {
		return err
	}
	if err := tx.verify(ctx, factory); err != nil {
		return err
	}
	switch {
	case a.nonce == math.MaxUint64:
		// For this error to occur, a user would need to be issuing transactions
		// at 10k tps for ~ 80 million years
		return errOutOfSpends
	case a.nonce+1 != tx.nonce:
		return fmt.Errorf("wrong tx nonce used, %d != %d", a.nonce+1, tx.nonce)
	case a.balance < tx.amount:
		return fmt.Errorf("%s %d < %d", errInsufficientFunds, a.balance, tx.amount)
	case a.nonce+1 == math.MaxUint64 && a.balance != tx.amount:
		return errOutOfSpends
	case !a.id.Equals(tx.key(ctx, factory).Address()):
		return errInvalidAddress
	default:
		return nil
	}
}

// Receive generates a new account state from receiving the transaction
func (a Account) Receive(tx *Tx, ctx *snow.Context) (Account, error) {
	return a.receive(tx, ctx, &crypto.FactorySECP256K1R{})
}

func (a Account) receive(tx *Tx, ctx *snow.Context, factory *crypto.FactorySECP256K1R) (Account, error) {
	return Account{
		id:    a.id,
		nonce: a.nonce,
		// guaranteed not to overflow due to VerifyReceive
		balance: a.balance + tx.amount,
	}, a.verifyReceive(tx, ctx, factory)
}

// VerifyReceive returns if the provided transaction can receive this
// transaction
func (a Account) VerifyReceive(tx *Tx, ctx *snow.Context) error {
	return a.verifyReceive(tx, ctx, &crypto.FactorySECP256K1R{})
}

func (a Account) verifyReceive(tx *Tx, ctx *snow.Context, factory *crypto.FactorySECP256K1R) error {
	if err := a.Verify(); err != nil {
		return err
	}
	if err := tx.verify(ctx, factory); err != nil {
		return err
	}
	switch {
	case a.nonce == math.MaxUint64:
		// For this error to occur, a user would need to be issuing transactions
		// at 10k tps for ~ 80 million years
		return errOutOfSpends
	case a.balance > math.MaxUint64-tx.amount:
		return errOverflow
	case !a.id.Equals(tx.to):
		return errInvalidID
	default:
		return nil
	}
}

// Verify that this account is well formed
func (a Account) Verify() error {
	switch {
	case a.id.IsZero():
		return errInvalidID
	default:
		return nil
	}
}

func (a Account) String() string {
	return fmt.Sprintf("Account[%s]: Balance=%d, Nonce=%d", a.ID(), a.Balance(), a.Nonce())
}
