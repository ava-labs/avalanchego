// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package libevmextras

import (
	"encoding/hex"
	"math/big"
	"reflect"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
)

func TestHeaderRLP(t *testing.T) {
	t.Parallel()

	header, _ := headerWithNonZeroFields()
	got, err := rlp.EncodeToBytes(header)
	require.NoError(t, err, "rlp.EncodeToBytes(header)")

	// Golden data from the C-Chain codec that predates libevm. WARNING: changing
	// these values can break backwards compatibility with extreme consequences
	// as block-hash calculation may break.
	const wantHex = "f9023ca00100000000000000000000000000000000000000000000000000000000000000a00200000000000000000000000000000000000000000000000000000000000000940300000000000000000000000000000000000000a00400000000000000000000000000000000000000000000000000000000000000a00500000000000000000000000000000000000000000000000000000000000000a00600000000000000000000000000000000000000000000000000000000000000b901000700000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000008090a0b0c0da00e00000000000000000000000000000000000000000000000000000000000000880f00000000000000a015000000000000000000000000000000000000000000000000000000000000001016171213a0140000000000000000000000000000000000000000000000000000000000000018191a1b1c1d1e1f"

	require.Equal(t, wantHex, hex.EncodeToString(got), "header RLP")

	decoded := new(types.Header)
	require.NoError(t, rlp.DecodeBytes(got, decoded), "rlp.DecodeBytes(header RLP)")
	wantHeader, wantExtra := headerWithNonZeroFields()
	wantHeader.WithdrawalsHash = nil // never on the wire
	require.Equal(t, wantHeader, decoded, "decoded header")
	require.Equal(t, wantExtra, GetHeader(decoded), "decoded HeaderExtra")
}

func TestHeaderTimeMilliseconds(t *testing.T) {
	t.Parallel()

	h := WithHeader(&types.Header{Time: 12}, &Header{})
	require.Equal(t, uint64(12_000), HeaderTimeMilliseconds(h), "pre-Granite falls back to Time*1000")
	GetHeader(h).TimeMilliseconds = new(uint64(12_345))
	require.Equal(t, uint64(12_345), HeaderTimeMilliseconds(h), "Granite TimeMilliseconds wins")
}

func TestHeaderWithNonZeroFields(t *testing.T) {
	t.Parallel()

	header, extra := headerWithNonZeroFields()
	t.Run("Header", func(t *testing.T) { allFieldsSet(t, header) })
	t.Run("HeaderExtra", func(t *testing.T) { allFieldsSet(t, extra) })
}

// headerWithNonZeroFields returns a [Header] and a [Header],
// each with all fields set to non-zero values.
// The [Header] extra payload is set in the [Header] via [WithHeader].
//
// NOTE: They can be used to demonstrate that RLP and JSON round-trip encoding
// can recover all fields, but not that the encoded format is correct. This is
// very important as the RLP encoding of a [Header] defines its hash.
func headerWithNonZeroFields() (*types.Header, *Header) {
	header := &types.Header{
		ParentHash:       common.Hash{1},
		UncleHash:        common.Hash{2},
		Coinbase:         common.Address{3},
		Root:             common.Hash{4},
		TxHash:           common.Hash{5},
		ReceiptHash:      common.Hash{6},
		Bloom:            types.Bloom{7},
		Difficulty:       big.NewInt(8),
		Number:           big.NewInt(9),
		GasLimit:         10,
		GasUsed:          11,
		Time:             12,
		Extra:            []byte{13},
		MixDigest:        common.Hash{14},
		Nonce:            types.BlockNonce{15},
		BaseFee:          big.NewInt(16),
		WithdrawalsHash:  &common.Hash{17},
		BlobGasUsed:      new(uint64(18)),
		ExcessBlobGas:    new(uint64(19)),
		ParentBeaconRoot: &common.Hash{20},
	}
	extra := &Header{
		ExtDataHash:         common.Hash{21},
		ExtDataGasUsed:      big.NewInt(22),
		BlockGasCost:        big.NewInt(23),
		TimeMilliseconds:    new(uint64(24)),
		MinDelayExcess:      new(dynamic.DelayExponent(25)),
		TargetExponent:      new(dynamic.TargetExponent(26)),
		MinPriceExponent:    new(dynamic.PriceExponent(27)),
		SettledHeight:       new(uint64(28)),
		SettledGasUnix:      new(uint64(29)),
		SettledGasNumerator: new(uint64(30)),
		SettledExcess:       new(uint64(31)),
	}
	return WithHeader(header, extra), extra
}

// allFieldsSet checks that a test fixture sets every exported field, so that
// the round-trip tests using it cover each one.
func allFieldsSet[T types.Header | Header | BlockBody](t *testing.T, x *T) {
	v := reflect.ValueOf(x).Elem()
	for i := range v.Type().NumField() {
		field := v.Type().Field(i)
		if !field.IsExported() {
			continue
		}

		t.Run(field.Name, func(t *testing.T) {
			fieldValue := v.Field(i)
			if fieldValue.Kind() == reflect.Slice {
				require.NotZerof(t, fieldValue.Len(), "field %q is empty", field.Name)
			} else {
				require.Falsef(t, fieldValue.IsZero(), "field %q is zero", field.Name)
			}
		})
	}
}

// Note [TestCopyHeader] tests the [HeaderExtra.PostCopy] method.
