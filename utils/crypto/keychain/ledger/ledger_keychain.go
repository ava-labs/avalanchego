// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ledger

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ keychain.Keychain = (*ledgerKeychain)(nil)
	_ keychain.Signer   = (*ledgerSigner)(nil)

	ErrInvalidIndicesLength    = errors.New("number of indices should be greater than 0")
	ErrInvalidNumAddrsToDerive = errors.New("number of addresses to derive should be greater than 0")
	ErrInvalidNumAddrsDerived  = errors.New("incorrect number of ledger derived addresses")
	ErrInvalidNumSignatures    = errors.New("incorrect number of signatures")
)

// ledgerKeychain is an abstraction of the underlying ledger hardware device,
// to be able to get a signer from a finite set of derived signers
type ledgerKeychain struct {
	ledger    Ledger
	addrs     set.Set[ids.ShortID]
	addrToIdx map[ids.ShortID]uint32
}

// NewLedgerKeychain creates a new Ledger with [numToDerive] addresses.
func NewLedgerKeychain(l Ledger, numToDerive int) (keychain.Keychain, error) {
	if numToDerive < 1 {
		return nil, ErrInvalidNumAddrsToDerive
	}

	indices := make([]uint32, numToDerive)
	for i := range indices {
		indices[i] = uint32(i)
	}

	return NewLedgerKeychainFromIndices(l, indices)
}

// NewLedgerKeychainFromIndices creates a new Ledger with addresses taken from the given [indices].
func NewLedgerKeychainFromIndices(l Ledger, indices []uint32) (keychain.Keychain, error) {
	if len(indices) == 0 {
		return nil, ErrInvalidIndicesLength
	}

	addrs, err := l.Addresses(indices)
	if err != nil {
		return nil, err
	}

	if len(addrs) != len(indices) {
		return nil, fmt.Errorf(
			"%w. expected %d, got %d",
			ErrInvalidNumAddrsDerived,
			len(indices),
			len(addrs),
		)
	}

	addrsSet := set.Of(addrs...)

	addrToIdx := map[ids.ShortID]uint32{}
	for i := range indices {
		addrToIdx[addrs[i]] = indices[i]
	}

	return &ledgerKeychain{
		ledger:    l,
		addrs:     addrsSet,
		addrToIdx: addrToIdx,
	}, nil
}

func (l *ledgerKeychain) Addresses() set.Set[ids.ShortID] {
	return l.addrs
}

func (l *ledgerKeychain) Get(addr ids.ShortID) (keychain.Signer, bool) {
	idx, ok := l.addrToIdx[addr]
	if !ok {
		return nil, false
	}

	return &ledgerSigner{
		ledger: l.ledger,
		idx:    idx,
		addr:   addr,
	}, true
}

// ledgerSigner is an abstraction of the underlying ledger hardware device,
// to be able sign for a specific address
type ledgerSigner struct {
	ledger Ledger
	idx    uint32
	addr   ids.ShortID
}

// expects to receive a hash of the unsigned tx bytes
func (l *ledgerSigner) SignHash(b []byte) ([]byte, error) {
	// Sign using the address with index l.idx on the ledger device. The number
	// of returned signatures should be the same length as the provided indices.
	sigs, err := l.ledger.SignHash(b, []uint32{l.idx})
	if err != nil {
		return nil, err
	}

	if sigsLen := len(sigs); sigsLen != 1 {
		return nil, fmt.Errorf(
			"%w. expected 1, got %d",
			ErrInvalidNumSignatures,
			sigsLen,
		)
	}

	return sigs[0], nil
}

// expects to receive the unsigned tx bytes
func (l *ledgerSigner) Sign(b []byte, opts ...keychain.SigningOption) ([]byte, error) {
	// Ignore options - ledger signing doesn't need chain/network context
	// Sign using the address with index l.idx on the ledger device. The number
	// of returned signatures should be the same length as the provided indices.
	sigs, err := l.ledger.Sign(b, []uint32{l.idx})
	if err != nil {
		return nil, err
	}

	if sigsLen := len(sigs); sigsLen != 1 {
		return nil, fmt.Errorf(
			"%w. expected 1, got %d",
			ErrInvalidNumSignatures,
			sigsLen,
		)
	}

	return sigs[0], nil
}

func (l *ledgerSigner) Address() ids.ShortID {
	return l.addr
}
