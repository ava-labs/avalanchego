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
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

var errTest = errors.New("test")

func TestNewLedgerKeychain(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	pubKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// user request invalid number of addresses to derive
	ledger := keychainmock.NewLedger(ctrl)
	_, err = NewLedgerKeychain(ledger, 0)
	require.ErrorIs(err, ErrInvalidNumAddrsToDerive)

	// ledger does not return expected number of derived addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{}, nil).Times(1)
	_, err = NewLedgerKeychain(ledger, 1)
	require.ErrorIs(err, ErrInvalidNumAddrsDerived)

	// ledger return error when asked for derived addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey.PublicKey()}, errTest).Times(1)
	_, err = NewLedgerKeychain(ledger, 1)
	require.ErrorIs(err, errTest)

	// good path
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey.PublicKey()}, nil).Times(1)
	_, err = NewLedgerKeychain(ledger, 1)
	require.NoError(err)
}

func TestLedgerKeychain_Addresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	key1, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key2, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key3, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	pubKey1 := key1.PublicKey()
	pubKey2 := key2.PublicKey()
	pubKey3 := key3.PublicKey()

	// 1 addr
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	addrs := kc.Addresses()
	require.Len(addrs, 1)
	require.True(addrs.Contains(pubKey1.Address()))

	// multiple addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0, 1, 2}).Return([]*secp256k1.PublicKey{pubKey1, pubKey2, pubKey3}, nil).Times(1)
	kc, err = NewLedgerKeychain(ledger, 3)
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, 3)
	require.Contains(addrs, pubKey1.Address())
	require.Contains(addrs, pubKey2.Address())
	require.Contains(addrs, pubKey3.Address())
}

func TestLedgerKeychain_Get(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	key1, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key2, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key3, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	pubKey1 := key1.PublicKey()
	pubKey2 := key2.PublicKey()
	pubKey3 := key3.PublicKey()

	addr1 := pubKey1.Address()
	addr2 := pubKey2.Address()
	addr3 := pubKey3.Address()

	// 1 addr
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	_, b := kc.Get(ids.GenerateTestShortID())
	require.False(b)

	s, b := kc.Get(addr1)
	require.Equal(s.Address(), addr1)
	require.True(b)

	// multiple addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0, 1, 2}).Return([]*secp256k1.PublicKey{pubKey1, pubKey2, pubKey3}, nil).Times(1)
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

	key1, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key2, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key3, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	pubKey1 := key1.PublicKey()
	pubKey2 := key2.PublicKey()
	pubKey3 := key3.PublicKey()

	addr1 := pubKey1.Address()
	addr2 := pubKey2.Address()
	addr3 := pubKey3.Address()

	toSign := []byte{1, 2, 3, 4, 5}
	expectedSignature1 := []byte{1, 1, 1}
	expectedSignature2 := []byte{2, 2, 2}
	expectedSignature3 := []byte{3, 3, 3}

	// ledger returns an incorrect number of signatures
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{}, nil).Times(1)
	kc, err := NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	s, b := kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, ErrInvalidNumSignatures)

	// ledger returns an error when asked for signature
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, errTest).Times(1)
	kc, err = NewLedgerKeychain(ledger, 1)
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, errTest)

	// good path 1 addr
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
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
	ledger.EXPECT().PubKeys([]uint32{0, 1, 2}).Return([]*secp256k1.PublicKey{pubKey1, pubKey2, pubKey3}, nil).Times(1)
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

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	pubKey := key.PublicKey()

	// user request invalid number of indices
	ledger := keychainmock.NewLedger(ctrl)
	_, err = NewLedgerKeychainFromIndices(ledger, []uint32{})
	require.ErrorIs(err, ErrInvalidIndicesLength)

	// ledger does not return expected number of derived addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{}, nil).Times(1)
	_, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.ErrorIs(err, ErrInvalidNumAddrsDerived)

	// ledger return error when asked for derived addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey}, errTest).Times(1)
	_, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.ErrorIs(err, errTest)

	// good path
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey}, nil).Times(1)
	_, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)
}

func TestLedgerKeychainFromIndices_Addresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	key1, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key2, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key3, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	pubKey1 := key1.PublicKey()
	pubKey2 := key2.PublicKey()
	pubKey3 := key3.PublicKey()

	addr1 := pubKey1.Address()
	addr2 := pubKey2.Address()
	addr3 := pubKey3.Address()

	// 1 addr
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
	kc, err := NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	addrs := kc.Addresses()
	require.Len(addrs, 1)
	require.True(addrs.Contains(addr1))

	// first 3 addresses
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0, 1, 2}).Return([]*secp256k1.PublicKey{pubKey1, pubKey2, pubKey3}, nil).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, []uint32{0, 1, 2})
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, 3)
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)

	// some 3 addresses
	indices := []uint32{3, 7, 1}
	pubKeys := []*secp256k1.PublicKey{pubKey1, pubKey2, pubKey3}
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys(indices).Return(pubKeys, nil).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, indices)
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, len(indices))
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)

	// repeated addresses
	indices = []uint32{3, 7, 1, 3, 1, 7}
	pubKeys = []*secp256k1.PublicKey{pubKey1, pubKey2, pubKey3, pubKey1, pubKey2, pubKey3}
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys(indices).Return(pubKeys, nil).Times(1)
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

	key1, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key2, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key3, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	pubKey1 := key1.PublicKey()
	pubKey2 := key2.PublicKey()
	pubKey3 := key3.PublicKey()

	addr1 := pubKey1.Address()
	addr2 := pubKey2.Address()
	addr3 := pubKey3.Address()

	// 1 addr
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
	kc, err := NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	_, b := kc.Get(ids.GenerateTestShortID())
	require.False(b)

	s, b := kc.Get(addr1)
	require.Equal(s.Address(), addr1)
	require.True(b)

	// some 3 addresses
	indices := []uint32{3, 7, 1}
	pubKeys := []*secp256k1.PublicKey{pubKey1, pubKey2, pubKey3}
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys(indices).Return(pubKeys, nil).Times(1)
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

	key1, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key2, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	key3, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	pubKey1 := key1.PublicKey()
	pubKey2 := key2.PublicKey()
	pubKey3 := key3.PublicKey()

	addr1 := pubKey1.Address()
	addr2 := pubKey2.Address()
	addr3 := pubKey3.Address()

	toSign := []byte{1, 2, 3, 4, 5}
	expectedSignature1 := []byte{1, 1, 1}
	expectedSignature2 := []byte{2, 2, 2}
	expectedSignature3 := []byte{3, 3, 3}

	// ledger returns an incorrect number of signatures
	ledger := keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{}, nil).Times(1)
	kc, err := NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	s, b := kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, ErrInvalidNumSignatures)

	// ledger returns an error when asked for signature
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, errTest).Times(1)
	kc, err = NewLedgerKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, errTest)

	// good path 1 addr
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys([]uint32{0}).Return([]*secp256k1.PublicKey{pubKey1}, nil).Times(1)
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
	pubKeys := []*secp256k1.PublicKey{pubKey1, pubKey2, pubKey3}
	ledger = keychainmock.NewLedger(ctrl)
	ledger.EXPECT().PubKeys(indices).Return(pubKeys, nil).Times(1)
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
