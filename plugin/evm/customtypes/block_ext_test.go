// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"math/big"
	"reflect"
	"testing"
	"unsafe"

	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/subnet-evm/internal/blocktest"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/ava-labs/subnet-evm/utils/utilstest"
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
			assert.Equal(t, test.wantBlockGasCost, blockGasCost, "BlockGasCost()")

			timeMilliseconds := BlockTimeMilliseconds(block)
			assert.Equal(t, test.wantTimeMilliseconds, timeMilliseconds, "BlockTimeMilliseconds()")

			minDelayExcess := BlockMinDelayExcess(block)
			assert.Equal(t, test.wantMinDelayExcess, minDelayExcess, "BlockMinDelayExcess()")
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

		assert.Equal(t, want, cpy)
	})

	t.Run("filled_header", func(t *testing.T) {
		t.Parallel()

		header, _ := headerWithNonZeroFields() // the header carries the [HeaderExtra] so we can ignore it

		gotHeader := CopyHeader(header)
		gotExtra := GetHeaderExtra(gotHeader)

		wantHeader, wantExtra := headerWithNonZeroFields()
		assert.Equal(t, wantHeader, gotHeader)
		assert.Equal(t, wantExtra, gotExtra)

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
				t.Errorf("field %q type %T needs to be added to switch cases of exportedFieldsDeepCopied", field.Name, f)
			}
		})
	}
}

// assertDifferentPointers asserts that `a` and `b` are both non-nil
// pointers pointing to different memory locations.
func assertDifferentPointers[T any](t *testing.T, a *T, b any) {
	t.Helper()
	switch {
	case a == nil:
		t.Errorf("a (%T) cannot be nil", a)
	case b == nil:
		t.Errorf("b (%T) cannot be nil", b)
	case a == b:
		t.Errorf("pointers to same memory")
	}
	// Note: no need to check `b` is of the same type as `a`, otherwise
	// the memory address would be different as well.
}
