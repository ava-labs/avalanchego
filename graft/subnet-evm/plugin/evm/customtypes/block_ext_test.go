// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"math/big"
	"reflect"
	"testing"
	"unsafe"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/internal/blocktest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
)

func TestBlockGetters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		headerExtra          *HeaderExtra
		wantBlockGasCost     *big.Int
		wantTimeMilliseconds *uint64
		wantMinDelayExcess   *acp226.DelayExcess
	}{
		{
			name:                 "empty",
			headerExtra:          &HeaderExtra{},
			wantTimeMilliseconds: nil,
			wantMinDelayExcess:   nil,
		},
		{
			name: "fields_set",
			headerExtra: &HeaderExtra{
				BlockGasCost:     big.NewInt(2),
				TimeMilliseconds: utils.NewUint64(3),
				MinDelayExcess:   utilstest.PointerTo(acp226.DelayExcess(4)),
			},
			wantBlockGasCost:     big.NewInt(2),
			wantTimeMilliseconds: utils.NewUint64(3),
			wantMinDelayExcess:   utilstest.PointerTo(acp226.DelayExcess(4)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			header := WithHeaderExtra(&Header{}, test.headerExtra)

			block := NewBlock(header, nil, nil, nil, blocktest.NewHasher())

			blockGasCost := BlockGasCost(block)
			require.Equal(t, test.wantBlockGasCost, blockGasCost, "BlockGasCost()")

			timeMilliseconds := BlockTimeMilliseconds(block)
			require.Equal(t, test.wantTimeMilliseconds, timeMilliseconds, "BlockTimeMilliseconds()")

			minDelayExcess := BlockMinDelayExcess(block)
			require.Equal(t, test.wantMinDelayExcess, minDelayExcess, "BlockMinDelayExcess()")
		})
	}
}

func TestCopyHeader(t *testing.T) {
	t.Parallel()

	t.Run("empty_header", func(t *testing.T) {
		t.Parallel()

		empty := &Header{}

		headerExtra := &HeaderExtra{}
		extras.Header.Set(empty, headerExtra)

		cpy := CopyHeader(empty)

		want := &Header{
			Difficulty: new(big.Int),
			Number:     new(big.Int),
		}

		headerExtra = &HeaderExtra{}
		extras.Header.Set(want, headerExtra)

		require.Equal(t, want, cpy)
	})

	t.Run("filled_header", func(t *testing.T) {
		t.Parallel()

		header, _ := headerWithNonZeroFields() // the header carries the [HeaderExtra] so we can ignore it

		gotHeader := CopyHeader(header)
		gotExtra := GetHeaderExtra(gotHeader)

		wantHeader, wantExtra := headerWithNonZeroFields()
		require.Equal(t, wantHeader, gotHeader)
		require.Equal(t, wantExtra, gotExtra)

		exportedFieldsPointToDifferentMemory(t, header, gotHeader)
		exportedFieldsPointToDifferentMemory(t, GetHeaderExtra(header), gotExtra)
	})
}

func exportedFieldsPointToDifferentMemory[T interface {
	Header | HeaderExtra
}](t *testing.T, original, cpy *T) {
	t.Helper()

	v := reflect.ValueOf(*original)
	typ := v.Type()
	cp := reflect.ValueOf(*cpy)
	for i := range v.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		switch field.Type.Kind() {
		case reflect.Array, reflect.Uint64:
			// Not pointers, but using explicit Kinds for safety
			continue
		}

		t.Run(field.Name, func(t *testing.T) {
			fieldCp := cp.Field(i).Interface()
			switch f := v.Field(i).Interface().(type) {
			case *big.Int:
				assertDifferentPointers(t, f, fieldCp)
			case *common.Hash:
				assertDifferentPointers(t, f, fieldCp)
			case *acp226.DelayExcess:
				assertDifferentPointers(t, f, fieldCp)
			case *uint64:
				assertDifferentPointers(t, f, fieldCp)
			case []uint8:
				assertDifferentPointers(t, unsafe.SliceData(f), unsafe.SliceData(fieldCp.([]uint8)))
			default:
				require.Failf(t, "field type needs to be added to switch cases", "field %q type %T needs to be added to switch cases of exportedFieldsDeepCopied", field.Name, f)
			}
		})
	}
}

// assertDifferentPointers asserts that `a` and `b` are both non-nil
// pointers pointing to different memory locations.
func assertDifferentPointers[T any](t *testing.T, a *T, b any) {
	t.Helper()
	require.NotNil(t, a, "a (%T) cannot be nil", a)
	require.NotNil(t, b, "b (%T) cannot be nil", b)
	require.NotSame(t, a, b, "pointers to same memory")
	// Note: no need to check `b` is of the same type as `a`, otherwise
	// the memory address would be different as well.
}
