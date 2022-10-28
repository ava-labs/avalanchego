// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"

	ledger "github.com/ava-labs/avalanche-ledger-go"
)

var (
	_ Keychain = (*ledgerKeychain)(nil)
	_ Signer   = (*ledgerSigner)(nil)

	ErrInvalidNumAddrsToDerive = errors.New("number of addresses to derive should be greater than 0")
	ErrInvalidNumAddrsDerived  = errors.New("incorrect number of ledger derived addresses")
	ErrInvalidNumSignatures    = errors.New("incorrect number of signatures")
)

// Signer implements functions for a keychain to return its main address and
// to sign a hash
type Signer interface {
	SignHash([]byte) ([]byte, error)
	Address() ids.ShortID
}

// Keychain maintains a set of addresses together with their corresponding
// signers
type Keychain interface {
	// The returned Signer can provide a signature for [addr]
	Get(addr ids.ShortID) (Signer, bool)
	// Returns the set of addresses for which the accessor keeps an associated
	// signer
	Addresses() ids.ShortSet
}

// ledgerKeychain is an abstraction of the underlying ledger hardware device,
// to be able to get a signer from a finite set of derived signers
type ledgerKeychain struct {
	ledger    ledger.Ledger
	addrs     ids.ShortSet
	addrToIdx map[ids.ShortID]uint32
}

// ledgerSigner is an abstraction of the underlying ledger hardware device,
// to be able sign for a specific address
type ledgerSigner struct {
	ledger ledger.Ledger
	idx    uint32
	addr   ids.ShortID
}

// NewLedgerKeychain creates a new Ledger with [numToDerive] addresses.
func NewLedgerKeychain(l ledger.Ledger, numToDerive int) (Keychain, error) {
	if numToDerive < 1 {
		return nil, ErrInvalidNumAddrsToDerive
	}

	addrs, err := l.Addresses(numToDerive)
	if err != nil {
		return nil, err
	}

	addrsLen := len(addrs)
	if addrsLen != numToDerive {
		return nil, fmt.Errorf(
			"%w. expected %d, got %d",
			ErrInvalidNumAddrsDerived,
			numToDerive,
			addrsLen,
		)
	}

	addrsSet := ids.NewShortSet(addrsLen)
	addrsSet.Add(addrs...)
	addrToIdx := make(map[ids.ShortID]uint32, addrsLen)
	for i, addr := range addrs {
		addrToIdx[addr] = uint32(i)
	}

	return &ledgerKeychain{
		ledger:    l,
		addrs:     addrsSet,
		addrToIdx: addrToIdx,
	}, nil
}

func (l *ledgerKeychain) Addresses() ids.ShortSet {
	return l.addrs
}

func (l *ledgerKeychain) Get(addr ids.ShortID) (Signer, bool) {
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

	return sigs[0], err
}

func (l *ledgerSigner) Address() ids.ShortID {
	return l.addr
}
