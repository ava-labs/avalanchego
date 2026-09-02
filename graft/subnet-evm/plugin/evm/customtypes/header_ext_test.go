// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/dynamic"
)

func TestMain(m *testing.M) {
	Register()
	os.Exit(m.Run())
}

func TestHeaderRLP(t *testing.T) {
	t.Parallel()

	got := testHeaderEncodeDecode(t, rlp.EncodeToBytes, rlp.DecodeBytes)

	// Golden data from original subnet-evm implementation, before integration of
	// libevm. WARNING: changing these values can break backwards compatibility
	// with extreme consequences as block-hash calculation may break.
	const (
		wantHex     = "f9021ea00100000000000000000000000000000000000000000000000000000000000000a00200000000000000000000000000000000000000000000000000000000000000940300000000000000000000000000000000000000a00400000000000000000000000000000000000000000000000000000000000000a00500000000000000000000000000000000000000000000000000000000000000a00600000000000000000000000000000000000000000000000000000000000000b901000700000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000008090a0b0c0da00e00000000000000000000000000000000000000000000000000000000000000880f0000000000000010171213a0140000000000000000000000000000000000000000000000000000000000000018191a1b1c1d1e1f20212223"
		wantHashHex = "b5f5499bb75d1cc2ee525c05b9ec1ab7e055f3531c38ad51820ca5414dc3269b"
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

func TestHeaderExtraPostRPCMarshal(t *testing.T) {
	header, extra := headerWithNonZeroFields()
	got := make(map[string]any)
	extra.PostRPCMarshal(header, got)

	want := map[string]any{
		"blockGasCost":                   (*hexutil.Big)(big.NewInt(23)),
		"timestampMilliseconds":          hexutil.Uint64(24),
		"minDelayExcess":                 hexutil.Uint64(25),
		"targetExcess":                   hexutil.Uint64(26),
		"settledHeight":                  hexutil.Uint64(27),
		"settledGasUnix":                 hexutil.Uint64(28),
		"settledGasNumerator":            hexutil.Uint64(29),
		"settledExcess":                  hexutil.Uint64(30),
		"gasConfigValidatorTargetGas":    hexutil.Uint64(31),
		"gasConfigTargetGas":             hexutil.Uint64(32),
		"gasConfigTargetToExcessScaling": hexutil.Uint64(33),
		"gasConfigMinGasPrice":           hexutil.Uint64(34),
		"gasConfigStaticPricing":         hexutil.Uint64(35),
	}
	require.Equal(t, want, got, "HeaderExtra.PostRPCMarshal()")
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

	gotHeader := new(types.Header)
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
func headerWithNonZeroFields() (*types.Header, *HeaderExtra) {
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
		BlobGasUsed:      utils.PointerTo[uint64](18),
		ExcessBlobGas:    utils.PointerTo[uint64](19),
		ParentBeaconRoot: &common.Hash{20},
	}
	extra := &HeaderExtra{
		BlockGasCost:                   big.NewInt(23),
		TimeMilliseconds:               utils.PointerTo[uint64](24),
		MinDelayExponent:               utils.PointerTo(dynamic.DelayExponent(25)),
		TargetExcess:                   utils.PointerTo(gas.Gas(26)),
		SettledHeight:                  utils.PointerTo[uint64](27),
		SettledGasUnix:                 utils.PointerTo[uint64](28),
		SettledGasNumerator:            utils.PointerTo[uint64](29),
		SettledExcess:                  utils.PointerTo[uint64](30),
		GasConfigValidatorTargetGas:    utils.PointerTo[uint64](31),
		GasConfigTargetGas:             utils.PointerTo[uint64](32),
		GasConfigTargetToExcessScaling: utils.PointerTo[uint64](33),
		GasConfigMinGasPrice:           utils.PointerTo[uint64](34),
		GasConfigStaticPricing:         utils.PointerTo[uint64](35),
	}
	return WithHeaderExtra(header, extra), extra
}

func allFieldsSet[T interface {
	types.Header | HeaderExtra
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
			case types.BlockNonce:
				assertNonZero(t, f)
			case types.Bloom:
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
			case *types.Header:
				assertNonZero(t, f)
			case *dynamic.DelayExponent:
				assertNonZero(t, f)
			case *gas.Gas:
				assertNonZero(t, f)
			case []uint8, []*types.Header, types.Transactions, []*types.Transaction, types.Withdrawals, []*types.Withdrawal:
				require.NotEmpty(t, f)
			default:
				t.Fatalf("Field %q has unsupported type %T", field.Name, f)
			}
		})
	}
}

func assertNonZero[T interface {
	common.Hash | common.Address | types.BlockNonce | uint32 | uint64 | types.Bloom |
		*big.Int | *common.Hash | *uint64 | *[]uint8 | *types.Header | *dynamic.DelayExponent | *gas.Gas
}](t *testing.T, v T) {
	t.Helper()
	require.NotZero(t, v)
}

// Note [TestCopyHeader] tests the [HeaderExtra.PostCopy] method.
