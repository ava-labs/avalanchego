// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"crypto/ecdsa"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/ethtest"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// A KeyChain manages a set of private keys (suitable only for tests) to sign
// transactions.
type KeyChain struct {
	keys  []*ecdsa.PrivateKey
	addrs []common.Address
}

// NewUNSAFEKeyChain returns a new key chain with the specified number of
// accounts. Private keys are generated deterministically.
func NewUNSAFEKeyChain(tb testing.TB, accounts uint) *KeyChain {
	tb.Helper()

	var (
		keys  []*ecdsa.PrivateKey
		addrs []common.Address
	)
	for i := range accounts {
		seed := binary.BigEndian.AppendUint64(nil, uint64(i))
		key := ethtest.UNSAFEDeterministicPrivateKey(tb, seed)
		keys = append(keys, key)
		addrs = append(addrs, crypto.PubkeyToAddress(key.PublicKey))
	}
	return &KeyChain{
		keys:  keys,
		addrs: addrs,
	}
}

// SignTx returns [types.SignNewTx], called with `data` and the respective
// `account` key.
func (kc *KeyChain) SignTx(tb testing.TB, signer types.Signer, account int, data types.TxData) *types.Transaction {
	tb.Helper()
	tx, err := types.SignNewTx(kc.keys[account], signer, data)
	require.NoError(tb, err, "types.SignNewTx(...)")
	return tx
}

// A Wallet manages a set of private keys (suitable only for tests) and nonces
// to sign transactions.
type Wallet struct {
	*KeyChain
	nonces []uint64 // MUST have same length as `kc.keys`
	signer types.Signer
}

// NewUNSAFEWallet returns a new wallet with the specified number of accounts.
// Private keys are generated deterministically.
func NewUNSAFEWallet(tb testing.TB, accounts uint, signer types.Signer) *Wallet {
	tb.Helper()
	return NewWalletWithKeyChain(NewUNSAFEKeyChain(tb, accounts), signer)
}

// NewWalletWithKeyChain returns a new wallet, backed by the provided key chain.
func NewWalletWithKeyChain(kc *KeyChain, signer types.Signer) *Wallet {
	return &Wallet{
		KeyChain: kc,
		nonces:   make([]uint64, len(kc.keys)),
		signer:   signer,
	}
}

// Addresses returns all addresses managed by the key chain.
func (kc *KeyChain) Addresses() []common.Address {
	return slices.Clone(kc.addrs)
}

// SetNonceAndSign overrides the nonce in the `data` with the next one for the
// account, then signs and returns the transaction. The wallet's record of the
// account nonce begins at zero and increments after every successful call to
// this method.
func (w *Wallet) SetNonceAndSign(tb testing.TB, account int, data types.TxData) *types.Transaction {
	tb.Helper()

	n := w.nonces[account]
	switch d := data.(type) {
	case *types.LegacyTx:
		d.Nonce = n
	case *types.AccessListTx:
		d.Nonce = n
	case *types.DynamicFeeTx:
		d.Nonce = n
	default:
		tb.Fatalf("Unsupported transaction type: %T", d)
	}

	tx := w.SignTx(tb, w.signer, account, data)
	w.nonces[account]++
	return tx
}

// DecrementNonce decrements the nonce of the specified account. This is useful
// for retrying transactions with updated parameters.
func (w *Wallet) DecrementNonce(tb testing.TB, account int) {
	tb.Helper()
	require.NotZerof(tb, w.nonces[account], "Nonce of account [%d] MUST be non-zero to decrement", account)
	w.nonces[account]--
}

// MaxAllocFor returns a genesis allocation with [MaxUint256] as the balance for
// all addresses provided.
func MaxAllocFor(addrs ...common.Address) types.GenesisAlloc {
	alloc := make(types.GenesisAlloc)
	for _, a := range addrs {
		alloc[a] = types.Account{
			Balance: new(uint256.Int).SetAllOne().ToBig(),
		}
	}
	return alloc
}
