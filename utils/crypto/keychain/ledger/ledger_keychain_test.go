// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ledger

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain/ledger/ledgermock"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var errTest = errors.New("test")

func TestNewKeychain(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr := ids.GenerateTestShortID()

	// user request invalid number of addresses to derive
	ledger := ledgermock.NewLedger(ctrl)
	_, err := NewKeychain(ledger, 0)
	require.ErrorIs(err, ErrInvalidNumAddrsToDerive)

	// ledger does not return expected number of derived addresses
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{}, nil).Times(1)
	_, err = NewKeychain(ledger, 1)
	require.ErrorIs(err, ErrInvalidNumAddrsDerived)

	// ledger return error when asked for derived addresses
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, errTest).Times(1)
	_, err = NewKeychain(ledger, 1)
	require.ErrorIs(err, errTest)

	// good path
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, nil).Times(1)
	_, err = NewKeychain(ledger, 1)
	require.NoError(err)
}

func TestKeyChain_Addresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()

	// 1 addr
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	kc, err := NewKeychain(ledger, 1)
	require.NoError(err)

	addrs := kc.Addresses()
	require.Len(addrs, 1)
	require.True(addrs.Contains(addr1))

	// multiple addresses
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0, 1, 2}).Return([]ids.ShortID{addr1, addr2, addr3}, nil).Times(1)
	kc, err = NewKeychain(ledger, 3)
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, 3)
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)
}

func TestKeyChain_Get(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()

	// 1 addr
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	kc, err := NewKeychain(ledger, 1)
	require.NoError(err)

	_, b := kc.Get(ids.GenerateTestShortID())
	require.False(b)

	s, b := kc.Get(addr1)
	require.Equal(s.Address(), addr1)
	require.True(b)

	// multiple addresses
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0, 1, 2}).Return([]ids.ShortID{addr1, addr2, addr3}, nil).Times(1)
	kc, err = NewKeychain(ledger, 3)
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
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{}, nil).Times(1)
	kc, err := NewKeychain(ledger, 1)
	require.NoError(err)

	s, b := kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, ErrInvalidNumSignatures)

	// ledger returns an error when asked for signature
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, errTest).Times(1)
	kc, err = NewKeychain(ledger, 1)
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, errTest)

	// good path 1 addr
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, nil).Times(1)
	kc, err = NewKeychain(ledger, 1)
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	signature, err := s.SignHash(toSign)
	require.NoError(err)
	require.Equal(expectedSignature1, signature)

	// good path 3 addr
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0, 1, 2}).Return([]ids.ShortID{addr1, addr2, addr3}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{1}).Return([][]byte{expectedSignature2}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{2}).Return([][]byte{expectedSignature3}, nil).Times(1)
	kc, err = NewKeychain(ledger, 3)
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

func TestNewKeychainFromIndices(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr := ids.GenerateTestShortID()
	_ = addr

	// user request invalid number of indices
	ledger := ledgermock.NewLedger(ctrl)
	_, err := NewKeychainFromIndices(ledger, []uint32{})
	require.ErrorIs(err, ErrInvalidIndicesLength)

	// ledger does not return expected number of derived addresses
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{}, nil).Times(1)
	_, err = NewKeychainFromIndices(ledger, []uint32{0})
	require.ErrorIs(err, ErrInvalidNumAddrsDerived)

	// ledger return error when asked for derived addresses
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, errTest).Times(1)
	_, err = NewKeychainFromIndices(ledger, []uint32{0})
	require.ErrorIs(err, errTest)

	// good path
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, nil).Times(1)
	_, err = NewKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)
}

func TestKeychainFromIndices_Addresses(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()

	// 1 addr
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	kc, err := NewKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	addrs := kc.Addresses()
	require.Len(addrs, 1)
	require.True(addrs.Contains(addr1))

	// first 3 addresses
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0, 1, 2}).Return([]ids.ShortID{addr1, addr2, addr3}, nil).Times(1)
	kc, err = NewKeychainFromIndices(ledger, []uint32{0, 1, 2})
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, 3)
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)

	// some 3 addresses
	indices := []uint32{3, 7, 1}
	addresses := []ids.ShortID{addr1, addr2, addr3}
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses(indices).Return(addresses, nil).Times(1)
	kc, err = NewKeychainFromIndices(ledger, indices)
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, len(indices))
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)

	// repeated addresses
	indices = []uint32{3, 7, 1, 3, 1, 7}
	addresses = []ids.ShortID{addr1, addr2, addr3, addr1, addr2, addr3}
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses(indices).Return(addresses, nil).Times(1)
	kc, err = NewKeychainFromIndices(ledger, indices)
	require.NoError(err)

	addrs = kc.Addresses()
	require.Len(addrs, 3)
	require.Contains(addrs, addr1)
	require.Contains(addrs, addr2)
	require.Contains(addrs, addr3)
}

func TestKeychainFromIndices_Get(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	addr3 := ids.GenerateTestShortID()

	// 1 addr
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	kc, err := NewKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	_, b := kc.Get(ids.GenerateTestShortID())
	require.False(b)

	s, b := kc.Get(addr1)
	require.Equal(s.Address(), addr1)
	require.True(b)

	// some 3 addresses
	indices := []uint32{3, 7, 1}
	addresses := []ids.ShortID{addr1, addr2, addr3}
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses(indices).Return(addresses, nil).Times(1)
	kc, err = NewKeychainFromIndices(ledger, indices)
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
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{}, nil).Times(1)
	kc, err := NewKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	s, b := kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, ErrInvalidNumSignatures)

	// ledger returns an error when asked for signature
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, errTest).Times(1)
	kc, err = NewKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	_, err = s.SignHash(toSign)
	require.ErrorIs(err, errTest)

	// good path 1 addr
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{0}).Return([][]byte{expectedSignature1}, nil).Times(1)
	kc, err = NewKeychainFromIndices(ledger, []uint32{0})
	require.NoError(err)

	s, b = kc.Get(addr1)
	require.True(b)

	signature, err := s.SignHash(toSign)
	require.NoError(err)
	require.Equal(expectedSignature1, signature)

	// good path some 3 addresses
	indices := []uint32{3, 7, 1}
	addresses := []ids.ShortID{addr1, addr2, addr3}
	ledger = ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses(indices).Return(addresses, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{indices[0]}).Return([][]byte{expectedSignature1}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{indices[1]}).Return([][]byte{expectedSignature2}, nil).Times(1)
	ledger.EXPECT().SignHash(toSign, []uint32{indices[2]}).Return([][]byte{expectedSignature3}, nil).Times(1)
	kc, err = NewKeychainFromIndices(ledger, indices)
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

func TestShouldUseSignHash(t *testing.T) {
	require := require.New(t)

	addr := ids.ShortID{
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
	}

	txID := ids.ID{
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	}

	avaxAssetID, err := ids.FromString("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
	require.NoError(err)

	baseTxData := txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    1,
			BlockchainID: ids.GenerateTestID(),
			Ins: []*avax.TransferableInput{
				{
					UTXOID: avax.UTXOID{
						TxID:        txID,
						OutputIndex: 1,
					},
					Asset: avax.Asset{
						ID: avaxAssetID,
					},
					In: &secp256k1fx.TransferInput{
						Amt: 1000000,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{0},
						},
					},
				},
			},
			Outs: []*avax.TransferableOutput{},
		},
	}

	testCases := []struct {
		name     string
		tx       txs.UnsignedTx
		expected bool
	}{
		{
			name: "TransferSubnetOwnershipTx should use SignHash",
			tx: &txs.TransferSubnetOwnershipTx{
				BaseTx: baseTxData,
				Subnet: ids.GenerateTestID(),
				Owner: &secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
				SubnetAuth: &secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
			},
			expected: true,
		},
		{
			name:     "BaseTx should not use SignHash",
			tx:       &txs.BaseTx{BaseTx: baseTxData.BaseTx},
			expected: false,
		},
		{
			name: "AddValidatorTx should not use SignHash",
			tx: &txs.AddValidatorTx{
				BaseTx:    baseTxData,
				StakeOuts: []*avax.TransferableOutput{},
				RewardsOwner: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
			expected: false,
		},
		{
			name: "AddSubnetValidatorTx should not use SignHash",
			tx: &txs.AddSubnetValidatorTx{
				BaseTx:     baseTxData,
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			expected: false,
		},
		{
			name: "AddDelegatorTx should not use SignHash",
			tx: &txs.AddDelegatorTx{
				BaseTx:    baseTxData,
				StakeOuts: []*avax.TransferableOutput{},
				DelegationRewardsOwner: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
			expected: false,
		},
		{
			name: "RemoveSubnetValidatorTx should not use SignHash",
			tx: &txs.RemoveSubnetValidatorTx{
				BaseTx:     baseTxData,
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			expected: false,
		},
		{
			name: "TransformSubnetTx should not use SignHash",
			tx: &txs.TransformSubnetTx{
				BaseTx:     baseTxData,
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			expected: false,
		},
		{
			name: "AddPermissionlessValidatorTx should not use SignHash",
			tx: &txs.AddPermissionlessValidatorTx{
				BaseTx:    baseTxData,
				Signer:    &signer.Empty{},
				StakeOuts: []*avax.TransferableOutput{},
				ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
				DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
			expected: false,
		},
		{
			name: "AddPermissionlessDelegatorTx should not use SignHash",
			tx: &txs.AddPermissionlessDelegatorTx{
				BaseTx:    baseTxData,
				StakeOuts: []*avax.TransferableOutput{},
				DelegationRewardsOwner: &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
			expected: false,
		},
		{
			name: "ConvertSubnetToL1Tx should not use SignHash",
			tx: &txs.ConvertSubnetToL1Tx{
				BaseTx:     baseTxData,
				SubnetAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
				Validators: []*txs.ConvertSubnetToL1Validator{},
			},
			expected: false,
		},
		{
			name:     "RegisterL1ValidatorTx should not use SignHash",
			tx:       &txs.RegisterL1ValidatorTx{BaseTx: baseTxData},
			expected: false,
		},
		{
			name:     "SetL1ValidatorWeightTx should not use SignHash",
			tx:       &txs.SetL1ValidatorWeightTx{BaseTx: baseTxData},
			expected: false,
		},
		{
			name:     "IncreaseL1ValidatorBalanceTx should not use SignHash",
			tx:       &txs.IncreaseL1ValidatorBalanceTx{BaseTx: baseTxData},
			expected: false,
		},
		{
			name: "DisableL1ValidatorTx should not use SignHash",
			tx: &txs.DisableL1ValidatorTx{
				BaseTx:      baseTxData,
				DisableAuth: &secp256k1fx.Input{SigIndices: []uint32{0}},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(_ *testing.T) {
			unsignedBytes, err := txs.Codec.Marshal(txs.CodecVersion, &tc.tx)
			require.NoError(err)
			result, err := shouldUseSignHash(unsignedBytes)
			require.NoError(err)
			require.Equal(tc.expected, result)
		})
	}

	// Test invalid bytes - should return error
	t.Run("Invalid bytes should return error", func(_ *testing.T) {
		_, err := shouldUseSignHash([]byte{0xFF, 0xFF, 0xFF})
		require.ErrorIs(err, codec.ErrUnknownVersion)
	})
}

func TestLedgerSigner_Sign_WithPChainTransferSubnetOwnership(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr := ids.GenerateTestShortID()
	expectedSignature := []byte{1, 1, 1}

	// Create a TransferSubnetOwnershipTx
	baseTxData := txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    1,
			BlockchainID: ids.GenerateTestID(),
			Ins:          []*avax.TransferableInput{},
			Outs:         []*avax.TransferableOutput{},
		},
	}

	var tx txs.UnsignedTx = &txs.TransferSubnetOwnershipTx{
		BaseTx: baseTxData,
		Subnet: ids.GenerateTestID(),
		Owner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		SubnetAuth: &secp256k1fx.Input{
			SigIndices: []uint32{0},
		},
	}

	unsignedBytes, err := txs.Codec.Marshal(txs.CodecVersion, &tx)
	require.NoError(err)

	// When signing with P-Chain alias, should use SignHash
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, nil).Times(1)
	ledger.EXPECT().SignHash(hashing.ComputeHash256(unsignedBytes), []uint32{0}).Return([][]byte{expectedSignature}, nil).Times(1)

	kc, err := NewKeychain(ledger, 1)
	require.NoError(err)

	s, b := kc.Get(addr)
	require.True(b)

	signature, err := s.Sign(unsignedBytes, keychain.WithChainAlias("P"))
	require.NoError(err)
	require.Equal(expectedSignature, signature)
}

func TestLedgerSigner_Sign_WithPChainNonTransferSubnetOwnership(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr := ids.GenerateTestShortID()
	expectedSignature := []byte{2, 2, 2}

	// Create a BaseTx
	var tx txs.UnsignedTx = &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    1,
			BlockchainID: ids.GenerateTestID(),
			Ins:          []*avax.TransferableInput{},
			Outs:         []*avax.TransferableOutput{},
		},
	}

	unsignedBytes, err := txs.Codec.Marshal(txs.CodecVersion, &tx)
	require.NoError(err)

	// When signing with P-Chain alias but NOT TransferSubnetOwnershipTx, should use Sign (not SignHash)
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, nil).Times(1)
	ledger.EXPECT().Sign(unsignedBytes, []uint32{0}).Return([][]byte{expectedSignature}, nil).Times(1)

	kc, err := NewKeychain(ledger, 1)
	require.NoError(err)

	s, b := kc.Get(addr)
	require.True(b)

	signature, err := s.Sign(unsignedBytes, keychain.WithChainAlias("P"))
	require.NoError(err)
	require.Equal(expectedSignature, signature)
}

func TestLedgerSigner_Sign_WithoutChainAlias(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr := ids.GenerateTestShortID()
	toSign := []byte{1, 2, 3, 4, 5}
	expectedSignature := []byte{3, 3, 3}

	// When signing without chain alias, should use Sign (not SignHash)
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, nil).Times(1)
	ledger.EXPECT().Sign(toSign, []uint32{0}).Return([][]byte{expectedSignature}, nil).Times(1)

	kc, err := NewKeychain(ledger, 1)
	require.NoError(err)

	s, b := kc.Get(addr)
	require.True(b)

	signature, err := s.Sign(toSign)
	require.NoError(err)
	require.Equal(expectedSignature, signature)
}

func TestLedgerSigner_Sign_WithNonPChain(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	addr := ids.GenerateTestShortID()
	toSign := []byte{1, 2, 3, 4, 5}
	expectedSignature := []byte{4, 4, 4}

	// When signing with non-P-Chain alias, should use Sign (not SignHash)
	ledger := ledgermock.NewLedger(ctrl)
	ledger.EXPECT().Addresses([]uint32{0}).Return([]ids.ShortID{addr}, nil).Times(1)
	ledger.EXPECT().Sign(toSign, []uint32{0}).Return([][]byte{expectedSignature}, nil).Times(1)

	kc, err := NewKeychain(ledger, 1)
	require.NoError(err)

	s, b := kc.Get(addr)
	require.True(b)

	signature, err := s.Sign(toSign, keychain.WithChainAlias("X"))
	require.NoError(err)
	require.Equal(expectedSignature, signature)
}
