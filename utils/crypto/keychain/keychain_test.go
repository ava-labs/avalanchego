// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanche-ledger-go/mocks"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"

	avaledger "github.com/ava-labs/avalanche-ledger-go"
)

var errTest = errors.New("test")

func TestNewLedgerKeychain(t *testing.T) {
	require := require.New(t)

	addr, err := avaledger.ToShortID(ids.GenerateTestShortID().Bytes())
	require.NoError(err)

	// user request invalid number of addresses to derive
	ledger := &mocks.Ledger{}
	_, err = NewLedgerKeychain(ledger, 0)
	require.Equal(err, ErrInvalidNumAddrsToDerive)

	// ledger does not return expected number of derived addresses
	ledger = &mocks.Ledger{}
	ledger.On("Addresses", 1).Return([]avaledger.ShortID{}, nil)
	_, err = NewLedgerKeychain(ledger, 1)
	require.ErrorIs(err, ErrInvalidNumAddrsDerived)

	// ledger return error when asked for derived addresses
	ledger = &mocks.Ledger{}
	ledger.On("Addresses", 1).Return([]avaledger.ShortID{addr}, errTest)
	_, err = NewLedgerKeychain(ledger, 1)
	require.Equal(err, errTest)

	// good path
	ledger = &mocks.Ledger{}
	ledger.On("Addresses", 1).Return([]avaledger.ShortID{addr}, nil)
	_, err = NewLedgerKeychain(ledger, 1)
	require.NoError(err)
}

func TestLedgerKeychain_Addresses(t *testing.T) {
	require := require.New(t)

	addr1 := ids.GenerateTestShortID()
	castedAddr1 := avaledger.ShortID(addr1)
	addr2 := ids.GenerateTestShortID()
	castedAddr2 := avaledger.ShortID(addr2)
	addr3 := ids.GenerateTestShortID()
	castedAddr3 := avaledger.ShortID(addr3)

	// 1 addr
	ledger := &mocks.Ledger{}
	ledger.On("Addresses", 1).Return([]avaledger.ShortID{castedAddr1}, nil)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	addrs := kc.Addresses()
	require.Len(addrs, 1)
	require.True(addrs.Contains(addr1))

	// multiple addresses
	ledger = &mocks.Ledger{}
	ledger.On("Addresses", 3).Return([]avaledger.ShortID{castedAddr1, castedAddr2, castedAddr3}, nil)
	kc, err = NewLedgerKeychain(ledger, 3)
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, 3)
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)
}

func TestLedgerKeychain_Get(t *testing.T) {
	require := require.New(t)

	addr1 := ids.GenerateTestShortID()
	castedAddr1 := avaledger.ShortID(addr1)
	addr2 := ids.GenerateTestShortID()
	castedAddr2 := avaledger.ShortID(addr2)
	addr3 := ids.GenerateTestShortID()
	castedAddr3 := avaledger.ShortID(addr3)

	// 1 addr
	ledger := &mocks.Ledger{}
	ledger.On("Addresses", 1).Return([]avaledger.ShortID{castedAddr1}, nil)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	_, b := kc.Get(ids.GenerateTestShortID())
	require.False(b)

	s, b := kc.Get(addr1)
	require.Equal(s.Address(), addr1)
	require.True(b)

	// multiple addresses
	ledger = &mocks.Ledger{}
	ledger.On("Addresses", 3).Return([]avaledger.ShortID{castedAddr1, castedAddr2, castedAddr3}, nil)
	kc, err = NewLedgerKeychain(ledger, 3)
	require.NoError(err)

	_, b = kc.Get(ids.GenerateTestShortID())
	require.False(b)

	s, b = kc.Get(addr1)
	require.True(b)
	require.Equal(s.Address(), addr1)

	s, b = kc.Get(addr2)
	require.True(b)
	require.Equal(s.Address(), addr2)

	s, b = kc.Get(addr3)
	require.True(b)
	require.Equal(s.Address(), addr3)
}

func TestLedgerSigner_SignHash(t *testing.T) {
	require := require.New(t)

	addr1 := ids.GenerateTestShortID()
	castedAddr1 := avaledger.ShortID(addr1)
	addr2 := ids.GenerateTestShortID()
	castedAddr2 := avaledger.ShortID(addr2)
	addr3 := ids.GenerateTestShortID()
	castedAddr3 := avaledger.ShortID(addr3)
	toSign := []byte{1, 2, 3, 4, 5}
	expectedSignature1 := []byte{1, 1, 1}
	expectedSignature2 := []byte{2, 2, 2}
	expectedSignature3 := []byte{3, 3, 3}

	// ledger returns an incorrect number of signatures
	ledger := &mocks.Ledger{}
	ledger.On("Addresses", 1).Return([]avaledger.ShortID{castedAddr1}, nil)
	ledger.On("SignHash", toSign, []uint32{0}).Return([][]byte{}, nil)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	s, b := kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, ErrInvalidNumSignatures)

	// ledger returns an error when asked for signature
	ledger = &mocks.Ledger{}
	ledger.On("Addresses", 1).Return([]avaledger.ShortID{castedAddr1}, nil)
	ledger.On("SignHash", toSign, []uint32{0}).Return([][]byte{expectedSignature1}, errTest)
	kc, err = NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.Equal(err, errTest)

	// good path 1 addr
	ledger = &mocks.Ledger{}
	ledger.On("Addresses", 1).Return([]avaledger.ShortID{castedAddr1}, nil)
	ledger.On("SignHash", toSign, []uint32{0}).Return([][]byte{expectedSignature1}, nil)
	kc, err = NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	signature, err := s.SignHash(toSign)
	require.NoError(err)
	require.Equal(expectedSignature1, signature)

	// good path 3 addr
	ledger = &mocks.Ledger{}
	ledger.On("Addresses", 3).Return([]avaledger.ShortID{castedAddr1, castedAddr2, castedAddr3}, nil)
	ledger.On("SignHash", toSign, []uint32{0}).Return([][]byte{expectedSignature1}, nil)
	ledger.On("SignHash", toSign, []uint32{1}).Return([][]byte{expectedSignature2}, nil)
	ledger.On("SignHash", toSign, []uint32{2}).Return([][]byte{expectedSignature3}, nil)
	kc, err = NewLedgerKeychain(ledger, 3)
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	signature, err = s.SignHash(toSign)
	require.NoError(err)
	require.Equal(expectedSignature1, signature)

	s, b = kc.Get(addr2)
	require.True(b)

	signature, err = s.SignHash(toSign)
	require.NoError(err)
	require.Equal(expectedSignature2, signature)

	s, b = kc.Get(addr3)
	require.True(b)

	signature, err = s.SignHash(toSign)
	require.NoError(err)
	require.Equal(expectedSignature3, signature)
}
