// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/crypto"
)

var (
	errNilTx          = errors.New("nil tx")
	errTxHasNoValue   = errors.New("tx has no value")
	errWrongNetworkID = errors.New("tx has wrong network ID")
	errWrongChainID   = errors.New("tx has wrong chain ID")
)

// Tx is a monetary transfer
type Tx struct {
	// The ID of this transaction
	id ids.ID

	networkID uint32

	// The ID of the chain this transaction was issued on.
	// Used to prevent replay attacks. Without this field, an attacker could
	// re-issue a transaction sent on another chain running the same vm.
	chainID ids.ID

	// The recipient of the transfered funds
	to ids.ShortID

	// The nonce of the transaction
	nonce uint64

	// The amount to be transfered
	amount uint64

	// The signature on this transaction (namely, on [bytes])
	sig []byte

	// The public key that authorized this transaction
	pubkey crypto.PublicKey

	// The byte representation of this transaction
	bytes []byte

	// Called when this transaction is decided
	onDecide func(choices.Status)

	startedVerification, finishedVerification bool
	verificationErr                           error
	verification                              chan error
}

// ID of this transaction
func (tx *Tx) ID() ids.ID { return tx.id }

// Nonce is the new nonce of the account this transaction is being sent from
func (tx *Tx) Nonce() uint64 { return tx.nonce }

// Amount is the number of units to transfer to the recipient
func (tx *Tx) Amount() uint64 { return tx.amount }

// To is the address this transaction is sending to
func (tx *Tx) To() ids.ShortID { return tx.to }

// Bytes is the byte representation of this transaction
func (tx *Tx) Bytes() []byte { return tx.bytes }

// Key returns the public key used to authorize this transaction
// Key may return nil if Verify returned an error
// This function also sets [tx]'s public key
func (tx *Tx) Key(ctx *snow.Context) crypto.PublicKey {
	return tx.key(ctx, &crypto.FactorySECP256K1R{})
}

func (tx *Tx) key(ctx *snow.Context, factory *crypto.FactorySECP256K1R) crypto.PublicKey {
	// Verify must be called to check this error and ensure that the public key is valid
	_ = tx.verify(ctx, factory) // Sets the public key, assuming this tx is valid
	return tx.pubkey
}

// Verify that this transaction is well formed
func (tx *Tx) Verify(ctx *snow.Context) error { return tx.verify(ctx, &crypto.FactorySECP256K1R{}) }

func (tx *Tx) verify(ctx *snow.Context, factory *crypto.FactorySECP256K1R) error {
	// Check if tx has already been verified
	if tx.finishedVerification {
		return tx.verificationErr
	}

	// past this point, we know verification has neither passed nor failed in the past
	tx.startVerify(ctx, factory)

	// Wait for verification to complete
	tx.verificationErr = <-tx.verification
	tx.finishedVerification = true
	return tx.verificationErr
}

func (tx *Tx) startVerify(ctx *snow.Context, factory *crypto.FactorySECP256K1R) {
	// See if verification has been started.
	// If not, start verification
	if !tx.startedVerification {
		tx.startedVerification = true
		go func(tx *Tx, ctx *snow.Context, factory *crypto.FactorySECP256K1R) {
			tx.verification <- tx.syncVerify(ctx, factory)
		}(tx, ctx, factory)
	}
}

func (tx *Tx) syncVerify(ctx *snow.Context, factory *crypto.FactorySECP256K1R) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.pubkey != nil:
		return nil
	case tx.amount == 0:
		return errTxHasNoValue
	case tx.networkID != ctx.NetworkID:
		return errWrongNetworkID
	case !tx.chainID.Equals(ctx.ChainID):
		return errWrongChainID
	}

	codec := Codec{}
	// The byte repr. of this transaction, unsigned
	unsignedBytes, err := codec.MarshalUnsignedTx(tx)
	if err != nil {
		return err
	}

	// Using [unsignedBytes] and [tx.sig], derive the public key
	// that authorized this transaction
	key, err := factory.RecoverPublicKey(unsignedBytes, tx.sig)
	if err != nil {
		return err
	}

	tx.pubkey = key
	return nil
}
