// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/libevm/common"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ Keychain = (*ledgerKeychain)(nil)
	_ Signer   = (*ledgerSigner)(nil)

	ErrInvalidIndicesLength    = errors.New("number of indices should be greater than 0")
	ErrInvalidNumAddrsToDerive = errors.New("number of addresses to derive should be greater than 0")
	ErrInvalidNumAddrsDerived  = errors.New("incorrect number of ledger derived addresses")
	ErrInvalidNumSignatures    = errors.New("incorrect number of signatures")
)

// Signer implements functions for a keychain to return its main address and
// to sign a hash
type Signer interface {
	SignHash([]byte) ([]byte, error)
	Sign([]byte) ([]byte, error)
	Address() ids.ShortID
}

// Keychain maintains a set of addresses together with their corresponding
// signers
type Keychain interface {
	// The returned Signer can provide a signature for [addr]
	Get(addr ids.ShortID) (Signer, bool)
	// Returns the set of addresses for which the accessor keeps an associated
	// signer
	Addresses() set.Set[ids.ShortID]
}

// keyInfo holds both the public key and ledger index for a ledger key.
type keyInfo struct {
	pubKey *secp256k1.PublicKey // The Avalanche public key obtained from ledger
	idx    uint32               // The ledger index
}

// ledgerKeychain is an abstraction of the underlying ledger hardware device,
// to be able to get a signer from a finite set of derived signers
type ledgerKeychain struct {
	ledger           Ledger
	avaAddrToKeyInfo map[ids.ShortID]*keyInfo    // Maps Avalanche addresses to key info
	ethAddrToKeyInfo map[common.Address]*keyInfo // Maps Ethereum addresses to key info
}

// ledgerSigner is an abstraction of the underlying ledger hardware device,
// to be able sign for a specific address
type ledgerSigner struct {
	ledger Ledger
	idx    uint32
	pubKey *secp256k1.PublicKey
}

// NewLedgerKeychain creates a new Ledger with [numToDerive] addresses.
func NewLedgerKeychain(l Ledger, numToDerive int) (Keychain, error) {
	if numToDerive < 1 {
		return nil, ErrInvalidNumAddrsToDerive
	}

	indices := make([]uint32, numToDerive)
	for i := range indices {
		if i > math.MaxUint32 {
			return nil, fmt.Errorf("index %d exceeds uint32 max", i)
		}
		indices[i] = uint32(i)
	}

	return NewLedgerKeychainFromIndices(l, indices)
}

// NewLedgerKeychainFromIndices creates a new Ledger with addresses taken from the given [indices].
func NewLedgerKeychainFromIndices(l Ledger, indices []uint32) (Keychain, error) {
	if len(indices) == 0 {
		return nil, ErrInvalidIndicesLength
	}

	pubKeys, err := l.PubKeys(indices)
	if err != nil {
		return nil, err
	}

	if len(pubKeys) != len(indices) {
		return nil, fmt.Errorf(
			"%w. expected %d, got %d",
			ErrInvalidNumAddrsDerived,
			len(indices),
			len(pubKeys),
		)
	}

	avaAddrToKeyInfo := map[ids.ShortID]*keyInfo{}
	ethAddrToKeyInfo := map[common.Address]*keyInfo{}
	for i := range indices {
		keyInf := &keyInfo{
			pubKey: pubKeys[i],
			idx:    indices[i],
		}
		avaAddrToKeyInfo[pubKeys[i].Address()] = keyInf
		ethAddrToKeyInfo[pubKeys[i].EthAddress()] = keyInf
	}

	return &ledgerKeychain{
		ledger:           l,
		avaAddrToKeyInfo: avaAddrToKeyInfo,
		ethAddrToKeyInfo: ethAddrToKeyInfo,
	}, nil
}

func (kc *ledgerKeychain) Addresses() set.Set[ids.ShortID] {
	return set.Of(maps.Keys(kc.avaAddrToKeyInfo)...)
}

func (kc *ledgerKeychain) Get(addr ids.ShortID) (Signer, bool) {
	keyInf, found := kc.avaAddrToKeyInfo[addr]
	if !found {
		return nil, false
	}
	return &ledgerSigner{
		ledger: kc.ledger,
		pubKey: keyInf.pubKey,
		idx:    keyInf.idx,
	}, true
}

// EthAddresses returns the set of Ethereum addresses that this keychain can sign for.
func (kc *ledgerKeychain) EthAddresses() set.Set[common.Address] {
	return set.Of(maps.Keys(kc.ethAddrToKeyInfo)...)
}

// GetEth returns a signer for the given Ethereum address, if it exists in this keychain.
func (kc *ledgerKeychain) GetEth(addr common.Address) (Signer, bool) {
	keyInf, found := kc.ethAddrToKeyInfo[addr]
	if !found {
		return nil, false
	}
	return &ledgerSigner{
		ledger: kc.ledger,
		pubKey: keyInf.pubKey,
		idx:    keyInf.idx,
	}, true
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
func (l *ledgerSigner) Sign(b []byte) ([]byte, error) {
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
	return l.pubKey.Address()
}
