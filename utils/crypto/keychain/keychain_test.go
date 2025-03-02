// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain/keychainmock"
)

var errTest = errors.New("test")

func TestNewLedgerKeychain(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr := ids.GenerateTestShortID()

	// user request invalid number of addresses to derive
	ledger := keychainmock.NewLedger(ctrl)
	_, err := NewLedgerKeychain(ledger, 0)
	require.ErrorIs(err, ErrInvalidNumAddrsToDerive)

	// ledger does not return expected number of derived addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{}, nil).Times(1)
	_, err = NewLedgerKeychain(ledger, 1)
	require.ErrorIs(err, ErrInvalidNumAddrsDerived)

	// ledger return error when asked for derived addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, errTest).Times(1)
	_, err = NewLedgerKeychain(ledger, 1)
	require.ErrorIs(err, errTest)

	// good path
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, nil).Times(1)
	_, err = NewLedgerKeychain(ledger, 1)
	require.NoError(err)
}

func TestLedgerKeychain_Addresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()

	// 1 addr
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	addrs := kc.Addresses()
	require.Len(addrs, 1)
	require.True(addrs.Contains(addr1))

	// multiple addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0, 1, 2}).Return([]ids.ShortID{addr1, addr2, addr3}, nil).Times(1)
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
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()

	// 1 addr
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	_, b := kc.Get(ids.GenerateTestShortID())
	require.False(b)

	s, b := kc.Get(addr1)
	require.Equal(s.Address(), addr1)
	require.True(b)

	// multiple addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0, 1, 2}).Return([]ids.ShortID{addr1, addr2, addr3}, nil).Times(1)
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
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()
	toSign := []byte{1, 2, 3, 4, 5}
	expectedSignature1 := []byte{1, 1, 1}
	expectedSignature2 := []byte{2, 2, 2}
	expectedSignature3 := []byte{3, 3, 3}

	// ledger returns an incorrect number of signatures
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{}, nil).Times(1)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	s, b := kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, ErrInvalidNumSignatures)

	// ledger returns an error when asked for signature
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, errTest).Times(1)
	kc, err = NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, errTest)

	// good path 1 addr
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, nil).Times(1)
	kc, err = NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	signature, err := s.SignHash(toSign)
	require.NoError(err)
	require.Equal(expectedSignature1, signature)

	// good path 3 addr
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0, 1, 2}).Return([]ids.ShortID{addr1, addr2, addr3}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{1}).Return([][]byte{expectedSignature2}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{2}).Return([][]byte{expectedSignature3}, nil).Times(1)
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

func TestNewLedgerKeychainFromIndices(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr := ids.GenerateTestShortID()
	_ = addr

	// user request invalid number of indices
	ledger := keychainmock.NewLedger(ctrl)
	_, err := NewLedgerKeychainFromIndices(ledger, []uint32{})
	require.ErrorIs(err, ErrInvalidIndicesLength)

	// ledger does not return expected number of derived addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{}, nil).Times(1)
	_, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.ErrorIs(err, ErrInvalidNumAddrsDerived)

	// ledger return error when asked for derived addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, errTest).Times(1)
	_, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.ErrorIs(err, errTest)

	// good path
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, nil).Times(1)
	_, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)
}

func TestLedgerKeychainFromIndices_Addresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()

	// 1 addr
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	kc, err := NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	addrs := kc.Addresses()
	require.Len(addrs, 1)
	require.True(addrs.Contains(addr1))

	// first 3 addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0, 1, 2}).Return([]ids.ShortID{addr1, addr2, addr3}, nil).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, []uint32{0, 1, 2})
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, 3)
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)

	// some 3 addresses
	indices := []uint32{3, 7, 1}
	addresses := []ids.ShortID{addr1, addr2, addr3}
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses(indices).Return(addresses, nil).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, indices)
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, len(indices))
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)

	// repeated addresses
	indices = []uint32{3, 7, 1, 3, 1, 7}
	addresses = []ids.ShortID{addr1, addr2, addr3, addr1, addr2, addr3}
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses(indices).Return(addresses, nil).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, indices)
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, 3)
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)
}

func TestLedgerKeychainFromIndices_Get(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()

	// 1 addr
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	kc, err := NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	_, b := kc.Get(ids.GenerateTestShortID())
	require.False(b)

	s, b := kc.Get(addr1)
	require.Equal(s.Address(), addr1)
	require.True(b)

	// some 3 addresses
	indices := []uint32{3, 7, 1}
	addresses := []ids.ShortID{addr1, addr2, addr3}
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses(indices).Return(addresses, nil).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, indices)
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

func TestLedgerSignerFromIndices_SignHash(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()
	toSign := []byte{1, 2, 3, 4, 5}
	expectedSignature1 := []byte{1, 1, 1}
	expectedSignature2 := []byte{2, 2, 2}
	expectedSignature3 := []byte{3, 3, 3}

	// ledger returns an incorrect number of signatures
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{}, nil).Times(1)
	kc, err := NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	s, b := kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, ErrInvalidNumSignatures)

	// ledger returns an error when asked for signature
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, errTest).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, errTest)

	// good path 1 addr
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, nil).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	signature, err := s.SignHash(toSign)
	require.NoError(err)
	require.Equal(expectedSignature1, signature)

	// good path some 3 addresses
	indices := []uint32{3, 7, 1}
	addresses := []ids.ShortID{addr1, addr2, addr3}
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().Addresses(indices).Return(addresses, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{indices[0]}).Return([][]byte{expectedSignature1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{indices[1]}).Return([][]byte{expectedSignature2}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{indices[2]}).Return([][]byte{expectedSignature3}, nil).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, indices)
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
