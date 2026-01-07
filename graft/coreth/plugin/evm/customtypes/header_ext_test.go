// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
)

func TestHeaderRLP(t *testing.T) {
	t.Parallel()

	got := testHeaderEncodeDecode(t, rlp.EncodeToBytes, rlp.DecodeBytes)

	// Golden data from original coreth implementation, before integration of
	// libevm. WARNING: changing these values can break backwards compatibility
	// with extreme consequences as block-hash calculation may break.
	const (
		wantHex     = "f90236a00100000000000000000000000000000000000000000000000000000000000000a00200000000000000000000000000000000000000000000000000000000000000940300000000000000000000000000000000000000a00400000000000000000000000000000000000000000000000000000000000000a00500000000000000000000000000000000000000000000000000000000000000a00600000000000000000000000000000000000000000000000000000000000000b901000700000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000008090a0b0c0da00e00000000000000000000000000000000000000000000000000000000000000880f00000000000000a015000000000000000000000000000000000000000000000000000000000000001016171213a014000000000000000000000000000000000000000000000000000000000000001819"
		wantHashHex = "be13d7b6f1242dd87477eee76a46f9fa58311bf459e0d49cac6862b187b3fe9c"
	)

	require.Equal(t, wantHex, hex.EncodeToString(got), "Header RLP")

	header, _ := headerWithNonZeroFields()
	gotHashHex := header.Hash().Hex()
	require.Equal(t, "0x"+wantHashHex, gotHashHex, "Header.Hash()")
}

func TestHeaderJSON(t *testing.T) {
	t.Parallel()

	// Note we ignore the returned encoded bytes because we don't
	// need to compare them to a JSON gold standard.
	_ = testHeaderEncodeDecode(t, json.Marshal, json.Unmarshal)
}

func testHeaderEncodeDecode(
	t *testing.T,
	encode func(any) ([]byte, error),
	decode func([]byte, any) error,
) (encoded []byte) {
	t.Helper()

	input, _ := headerWithNonZeroFields() // the Header carries the HeaderExtra so we can ignore it
	encoded, err := encode(input)
	require.NoError(t, err, "encode")

	gotHeader := new(Header)
	err = decode(encoded, gotHeader)
	require.NoError(t, err, "decode")
	gotExtra := GetHeaderExtra(gotHeader)

	wantHeader, wantExtra := headerWithNonZeroFields()
	wantHeader.WithdrawalsHash = nil
	require.Equal(t, wantHeader, gotHeader)
	require.Equal(t, wantExtra, gotExtra)

	return encoded
}

func TestHeaderWithNonZeroFields(t *testing.T) {
	t.Parallel()

	header, extra := headerWithNonZeroFields()
	t.Run("Header", func(t *testing.T) { allFieldsSet(t, header, "extra") })
	t.Run("HeaderExtra", func(t *testing.T) { allFieldsSet(t, extra) })
}

// headerWithNonZeroFields returns a [Header] and a [HeaderExtra],
// each with all fields set to non-zero values.
// The [HeaderExtra] extra payload is set in the [Header] via [WithHeaderExtra].
//
// NOTE: They can be used to demonstrate that RLP and JSON round-trip encoding
// can recover all fields, but not that the encoded format is correct. This is
// very important as the RLP encoding of a [Header] defines its hash.
func headerWithNonZeroFields() (*Header, *HeaderExtra) {
	header := &Header{
		ParentHash:       common.Hash{1},
		UncleHash:        common.Hash{2},
		Coinbase:         common.Address{3},
		Root:             common.Hash{4},
		TxHash:           common.Hash{5},
		ReceiptHash:      common.Hash{6},
		Bloom:            Bloom{7},
		Difficulty:       big.NewInt(8),
		Number:           big.NewInt(9),
		GasLimit:         10,
		GasUsed:          11,
		Time:             12,
		Extra:            []byte{13},
		MixDigest:        common.Hash{14},
		Nonce:            BlockNonce{15},
		BaseFee:          big.NewInt(16),
		WithdrawalsHash:  &common.Hash{17},
		BlobGasUsed:      utilstest.PointerTo(uint64(18)),
		ExcessBlobGas:    utilstest.PointerTo(uint64(19)),
		ParentBeaconRoot: &common.Hash{20},
	}
	extra := &HeaderExtra{
		ExtDataHash:      common.Hash{21},
		ExtDataGasUsed:   big.NewInt(22),
		BlockGasCost:     big.NewInt(23),
		TimeMilliseconds: utilstest.PointerTo(uint64(24)),
		MinDelayExcess:   utilstest.PointerTo(acp226.DelayExcess(25)),
	}
	return WithHeaderExtra(header, extra), extra
}

func allFieldsSet[T interface {
	Header | HeaderExtra | Block | Body | BlockBodyExtra
}](t *testing.T, x *T, ignoredFields ...string) {
	// We don't test for nil pointers because we're only confirming that
	// test-input data is well-formed. A panic due to a dereference will be
	// reported anyway.

	v := reflect.ValueOf(x).Elem()
	for i := range v.Type().NumField() {
		field := v.Type().Field(i)
		if slices.Contains(ignoredFields, field.Name) {
			continue
		}

		t.Run(field.Name, func(t *testing.T) {
			fieldValue := v.Field(i)
			if !field.IsExported() {
				// Note: we need to check unexported fields especially for [Block].
				if fieldValue.Kind() == reflect.Ptr {
					require.Falsef(t, fieldValue.IsNil(), "field %q is nil", field.Name)
				}
				fieldValue = reflect.NewAt(fieldValue.Type(), unsafe.Pointer(fieldValue.UnsafeAddr())).Elem()
			}

			switch f := fieldValue.Interface().(type) {
			case common.Hash:
				assertNonZero(t, f)
			case common.Address:
				assertNonZero(t, f)
			case BlockNonce:
				assertNonZero(t, f)
			case Bloom:
				assertNonZero(t, f)
			case uint32:
				assertNonZero(t, f)
			case uint64:
				assertNonZero(t, f)
			case *big.Int:
				assertNonZero(t, f)
			case *common.Hash:
				assertNonZero(t, f)
			case *uint64:
				assertNonZero(t, f)
			case *[]uint8:
				assertNonZero(t, f)
			case *Header:
				assertNonZero(t, f)
			case *acp226.DelayExcess:
				assertNonZero(t, f)
			case []uint8, []*Header, Transactions, []*Transaction, Withdrawals, []*Withdrawal:
				require.NotEmpty(t, f)
			default:
				require.Failf(t, "Field has unsupported type", "Field %q has unsupported type %T", field.Name, f)
			}
		})
	}
}

func assertNonZero[T interface {
	common.Hash | common.Address | BlockNonce | uint32 | uint64 | Bloom |
		*big.Int | *common.Hash | *uint64 | *[]uint8 | *Header | *acp226.DelayExcess
}](t *testing.T, v T) {
	t.Helper()
	require.NotZero(t, v)
}

// Note [TestCopyHeader] tests the [HeaderExtra.PostCopy] method.
