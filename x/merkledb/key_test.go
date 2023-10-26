// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestHasPartialByte(t *testing.T) {
	for _, branchFactor := range validBranchFactors {
		t.Run(fmt.Sprint(branchFactor), func(t *testing.T) {
			require := require.New(t)

			key := Key{}
			require.False(key.hasPartialByte())

			if branchFactor == BranchFactor256 {
				// Tokens are an entire byte so
				// there is never a partial byte.
				key = key.Append(NewToken(1, branchFactor))
				require.False(key.hasPartialByte())
				key = key.Append(NewToken(0, branchFactor))
				require.False(key.hasPartialByte())
				return
			}

			// Fill all but the last token of the first byte.
			for i := 0; i < 8-branchFactor.BitsPerToken(); i += branchFactor.BitsPerToken() {
				key = key.Append(NewToken(1, branchFactor))
				require.True(key.hasPartialByte())
			}

			// Fill the last token of the first byte.
			key = key.Append(NewToken(0, branchFactor))
			require.False(key.hasPartialByte())

			// Fill the first token of the second byte.
			key = key.Append(NewToken(0, branchFactor))
			require.True(key.hasPartialByte())
		})
	}
}

func Test_Key_Has_Prefix(t *testing.T) {
	type test struct {
		name           string
		keyA           func(BranchFactor) Key
		keyB           func(BranchFactor) Key
		isStrictPrefix bool
		isPrefix       bool
	}

	key := "Key"

	tests := []test{
		{
			name:           "equal keys",
			keyA:           func(BranchFactor) Key { return ToKey([]byte(key)) },
			keyB:           func(BranchFactor) Key { return ToKey([]byte(key)) },
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name: "one key has one fewer token",
			keyA: func(BranchFactor) Key { return ToKey([]byte(key)) },
			keyB: func(bf BranchFactor) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - bf.BitsPerToken())
			},
			isPrefix:       true,
			isStrictPrefix: true,
		},
		{
			name: "equal keys, both have one fewer token",
			keyA: func(bf BranchFactor) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - bf.BitsPerToken())
			},
			keyB: func(bf BranchFactor) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - bf.BitsPerToken())
			},
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name:           "different keys",
			keyA:           func(BranchFactor) Key { return ToKey([]byte{0xF7}) },
			keyB:           func(BranchFactor) Key { return ToKey([]byte{0xF0}) },
			isPrefix:       false,
			isStrictPrefix: false,
		},
		{
			name: "same bytes, different lengths",
			keyA: func(bf BranchFactor) Key {
				return ToKey([]byte{0x10, 0x00}).Take(bf.BitsPerToken())
			},
			keyB: func(bf BranchFactor) Key {
				return ToKey([]byte{0x10, 0x00}).Take(bf.BitsPerToken() * 2)
			},
			isPrefix:       false,
			isStrictPrefix: false,
		},
	}

	for _, tt := range tests {
		for _, tc := range validBranchFactors {
			t.Run(tt.name+" tc "+fmt.Sprint(tc), func(t *testing.T) {
				require := require.New(t)
				keyA := tt.keyA(tc)
				keyB := tt.keyB(tc)

				require.Equal(tt.isPrefix, keyA.HasPrefix(keyB))
				require.Equal(tt.isPrefix, keyA.iteratedHasPrefix(keyB, 0, tc.BitsPerToken()))
				require.Equal(tt.isStrictPrefix, keyA.HasStrictPrefix(keyB))
			})
		}
	}
}

func Test_Key_Skip(t *testing.T) {
	require := require.New(t)

	empty := Key{}
	require.Equal(ToKey([]byte{0}).Skip(8), empty)
	for _, bf := range validBranchFactors {
		if bf == BranchFactor256 {
			continue
		}
		shortKey := ToKey([]byte{0b0101_0101})
		longKey := ToKey([]byte{0b0101_0101, 0b0101_0101})
		for shift := 0; shift < 8; shift += bf.BitsPerToken() {
			skipKey := shortKey.Skip(shift)
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[0])

			skipKey = longKey.Skip(shift)
			require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skipKey.value[0])
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[1])
		}
	}

	skip := ToKey([]byte{0b0101_0101, 0b1010_1010}).Skip(BranchFactor256.BitsPerToken())
	require.Len(skip.value, 1)
	require.Equal(byte(0b1010_1010), skip.value[0])

	skip = ToKey([]byte{0b0101_0101, 0b1010_1010, 0b0101_0101}).Skip(BranchFactor256.BitsPerToken())
	require.Len(skip.value, 2)
	require.Equal(byte(0b1010_1010), skip.value[0])
	require.Equal(byte(0b0101_0101), skip.value[1])
}

func Test_Key_Take(t *testing.T) {
	require := require.New(t)

	require.Equal(ToKey([]byte{0}).Take(0), Key{})

	for _, bf := range validBranchFactors {
		if bf == BranchFactor256 {
			continue
		}
		key := ToKey([]byte{0b0101_0101})
		for length := bf.BitsPerToken(); length <= 8; length += bf.BitsPerToken() {
			take := key.Take(length)
			require.Equal(length, take.length)
			shift := 8 - length
			require.Equal(byte((0b0101_0101>>shift)<<shift), take.value[0])
		}
	}

	take := ToKey([]byte{0b0101_0101, 0b1010_1010}).Take(8)
	require.Len(take.value, 1)
	require.Equal(byte(0b0101_0101), take.value[0])
}

func Test_Key_Token(t *testing.T) {
	type test struct {
		name         string
		inputBytes   []byte
		branchFactor BranchFactor
		assertTokens func(*require.Assertions, Key)
	}

	tests := []test{
		{
			name:         "branch factor 2",
			inputBytes:   []byte{0b0_1_0_1_0_1_0_1, 0b1_0_1_0_1_0_1_0},
			branchFactor: BranchFactor2,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(0, BranchFactor2.BitsPerToken()))
				require.Equal(byte(1), key.Token(1, BranchFactor2.BitsPerToken()))
				require.Equal(byte(0), key.Token(2, BranchFactor2.BitsPerToken()))
				require.Equal(byte(1), key.Token(3, BranchFactor2.BitsPerToken()))
				require.Equal(byte(0), key.Token(4, BranchFactor2.BitsPerToken()))
				require.Equal(byte(1), key.Token(5, BranchFactor2.BitsPerToken()))
				require.Equal(byte(0), key.Token(6, BranchFactor2.BitsPerToken()))
				require.Equal(byte(1), key.Token(7, BranchFactor2.BitsPerToken())) // end first byte
				require.Equal(byte(1), key.Token(8, BranchFactor2.BitsPerToken()))
				require.Equal(byte(0), key.Token(9, BranchFactor2.BitsPerToken()))
				require.Equal(byte(1), key.Token(10, BranchFactor2.BitsPerToken()))
				require.Equal(byte(0), key.Token(11, BranchFactor2.BitsPerToken()))
				require.Equal(byte(1), key.Token(12, BranchFactor2.BitsPerToken()))
				require.Equal(byte(0), key.Token(13, BranchFactor2.BitsPerToken()))
				require.Equal(byte(1), key.Token(14, BranchFactor2.BitsPerToken()))
				require.Equal(byte(0), key.Token(15, BranchFactor2.BitsPerToken())) // end second byte
			},
		},
		{
			name:         "branch factor 4",
			inputBytes:   []byte{0b00_01_10_11, 0b11_10_01_00},
			branchFactor: BranchFactor4,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(0, BranchFactor4.BitsPerToken()))  // 00
				require.Equal(byte(1), key.Token(2, BranchFactor4.BitsPerToken()))  // 01
				require.Equal(byte(2), key.Token(4, BranchFactor4.BitsPerToken()))  // 10
				require.Equal(byte(3), key.Token(6, BranchFactor4.BitsPerToken()))  // 11 end first byte
				require.Equal(byte(3), key.Token(8, BranchFactor4.BitsPerToken()))  // 11
				require.Equal(byte(2), key.Token(10, BranchFactor4.BitsPerToken())) // 10
				require.Equal(byte(1), key.Token(12, BranchFactor4.BitsPerToken())) // 01
				require.Equal(byte(0), key.Token(14, BranchFactor4.BitsPerToken())) // 00 end second byte
			},
		},
		{
			name: "branch factor 16",
			inputBytes: []byte{
				0b0000_0001,
				0b0010_0011,
				0b0100_0101,
				0b0110_0111,
				0b1000_1001,
				0b1010_1011,
				0b1100_1101,
				0b1110_1111,
			},
			branchFactor: BranchFactor16,
			assertTokens: func(require *require.Assertions, key Key) {
				for i := 0; i < 16; i++ {
					require.Equal(byte(i), key.Token(i*BranchFactor16.BitsPerToken(), BranchFactor16.BitsPerToken()))
				}
			},
		},
	}

	for i := 0; i < 256; i++ {
		i := i
		tests = append(tests, test{
			name:         fmt.Sprintf("branch factor 256, byte %d", i),
			inputBytes:   []byte{byte(i)},
			branchFactor: BranchFactor256,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(i), key.Token(0, BranchFactor256.BitsPerToken()))
			},
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			key := ToKey(tt.inputBytes)
			tt.assertTokens(require, key)
		})
	}
}

func Test_Key_Append(t *testing.T) {
	require := require.New(t)

	key := ToKey([]byte{})
	for _, bf := range validBranchFactors {
		for i := 0; i < int(bf); i++ {
			appendedKey := key.Append(NewToken(byte(i), bf)).Append(NewToken(byte(i/2), bf))
			require.Equal(byte(i), appendedKey.Token(0, bf.BitsPerToken()))
			require.Equal(byte(i/2), appendedKey.Token(bf.BitsPerToken(), bf.BitsPerToken()))
		}
	}
}

func Test_Key_AppendExtend(t *testing.T) {
	require := require.New(t)

	key2 := ToKey([]byte{0b1000_0000}).Take(1)
	p := ToKey([]byte{0b01010101})
	extendedP := key2.AppendExtend(NewToken(0, BranchFactor2), p)
	require.Equal([]byte{0b10010101, 0b01000_000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(1, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(2, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(3, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(4, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(5, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(6, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(7, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(8, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(9, BranchFactor2.BitsPerToken()))

	p = ToKey([]byte{0b0101_0101, 0b1000_0000}).Take(9)
	extendedP = key2.AppendExtend(NewToken(0, BranchFactor2), p)
	require.Equal([]byte{0b1001_0101, 0b0110_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(1, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(2, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(3, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(4, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(5, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(6, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(7, BranchFactor2.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(8, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(9, BranchFactor2.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(10, BranchFactor2.BitsPerToken()))

	key4 := ToKey([]byte{0b0100_0000}).Take(2)
	p = ToKey([]byte{0b0101_0101})
	extendedP = key4.AppendExtend(NewToken(0, BranchFactor4), p)
	require.Equal([]byte{0b0100_0101, 0b0101_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor4.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(2, BranchFactor4.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(4, BranchFactor4.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(6, BranchFactor4.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(8, BranchFactor4.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(10, BranchFactor4.BitsPerToken()))

	key16 := ToKey([]byte{0b0001_0000}).Take(4)
	p = ToKey([]byte{0b0001_0001})
	extendedP = key16.AppendExtend(NewToken(0, BranchFactor16), p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor16.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(4, BranchFactor16.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(8, BranchFactor16.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(12, BranchFactor16.BitsPerToken()))

	p = ToKey([]byte{0b0001_0001, 0b0001_0001})
	extendedP = key16.AppendExtend(NewToken(0, BranchFactor16), p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor16.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(4, BranchFactor16.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(8, BranchFactor16.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(12, BranchFactor16.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(16, BranchFactor16.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(20, BranchFactor16.BitsPerToken()))

	key256 := ToKey([]byte{0b0000_0001})
	p = ToKey([]byte{0b0000_0001})
	extendedP = key256.AppendExtend(NewToken(0, BranchFactor256), p)
	require.Equal([]byte{0b0000_0001, 0b0000_0000, 0b0000_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor256.BitsPerToken()))
	require.Equal(byte(0), extendedP.Token(8, BranchFactor256.BitsPerToken()))
	require.Equal(byte(1), extendedP.Token(16, BranchFactor256.BitsPerToken()))
}

func TestKeyBytesNeeded(t *testing.T) {
	type test struct {
		BranchFactor
		bitLength   int
		bytesNeeded int
	}

	tests := []test{
		{
			bitLength:   7,
			bytesNeeded: 1,
		},
		{
			bitLength:   8,
			bytesNeeded: 1,
		},
		{
			bitLength:   9,
			bytesNeeded: 2,
		},
		{
			bitLength:   0,
			bytesNeeded: 0,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("branch factor %d, tokens length %d", tt.BranchFactor, tt.bitLength), func(t *testing.T) {
			require := require.New(t)
			require.Equal(tt.bytesNeeded, bytesNeeded(tt.bitLength))
		})
	}
}

func FuzzKeyAppendExtend(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		second []byte,
		tokenByte byte,
		forceFirstOdd bool,
		forceSecondOdd bool,
	) {
		require := require.New(t)
		for _, branchFactor := range validBranchFactors {
			key1 := ToKey(first)
			if forceFirstOdd && key1.length > branchFactor.BitsPerToken() {
				key1 = key1.Take(key1.length - branchFactor.BitsPerToken())
			}
			key2 := ToKey(second)
			if forceSecondOdd && key2.length > branchFactor.BitsPerToken() {
				key2 = key2.Take(key2.length - branchFactor.BitsPerToken())
			}
			token := byte(int(tokenByte) % int(branchFactor))
			extendedP := key1.AppendExtend(NewToken(token, branchFactor), key2)
			require.Equal(key1.length+key2.length+branchFactor.BitsPerToken(), extendedP.length)
			firstIndex := 0
			for ; firstIndex < key1.length; firstIndex += branchFactor.BitsPerToken() {
				require.Equal(key1.Token(firstIndex, branchFactor.BitsPerToken()), extendedP.Token(firstIndex, branchFactor.BitsPerToken()))
			}
			require.Equal(token, extendedP.Token(firstIndex, branchFactor.BitsPerToken()))
			firstIndex += branchFactor.BitsPerToken()
			for secondIndex := 0; secondIndex < key2.length; secondIndex += branchFactor.BitsPerToken() {
				require.Equal(key2.Token(secondIndex, branchFactor.BitsPerToken()), extendedP.Token(firstIndex+secondIndex, branchFactor.BitsPerToken()))
			}
		}
	})
}

func FuzzKeySkip(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		tokensToSkip uint,
	) {
		require := require.New(t)
		key1 := ToKey(first)
		for _, branchFactor := range validBranchFactors {
			// need bits to be a multiple of token size
			ubitsToSkip := tokensToSkip * uint(branchFactor.BitsPerToken())
			if ubitsToSkip >= uint(key1.length) {
				t.SkipNow()
			}
			bitsToSkip := int(ubitsToSkip)
			key2 := key1.Skip(bitsToSkip)
			require.Equal(key1.length-bitsToSkip, key2.length)
			for i := 0; i < key2.length; i += branchFactor.BitsPerToken() {
				require.Equal(key1.Token(bitsToSkip+i, branchFactor.BitsPerToken()), key2.Token(i, branchFactor.BitsPerToken()))
			}
		}
	})
}

func FuzzKeyTake(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		uTokensToTake uint,
	) {
		require := require.New(t)
		for _, tokenConfig := range validBranchFactors {
			key1 := ToKey(first)
			uBitsToTake := uTokensToTake * uint(tokenConfig.BitsPerToken())
			if uBitsToTake >= uint(key1.length) {
				t.SkipNow()
			}
			bitsToTake := int(uBitsToTake)
			key2 := key1.Take(bitsToTake)
			require.Equal(bitsToTake, key2.length)
			if key2.hasPartialByte() {
				paddingMask := byte(0xFF >> key2.remainderBitCount())
				require.Zero(key2.value[len(key2.value)-1] & paddingMask)
			}
			for i := 0; i < bitsToTake; i += tokenConfig.BitsPerToken() {
				require.Equal(key1.Token(i, tokenConfig.BitsPerToken()), key2.Token(i, tokenConfig.BitsPerToken()))
			}
		}
	})
}

func TestShiftCopy(t *testing.T) {
	type test struct {
		dst      []byte
		src      []byte
		expected []byte
		shift    int
	}

	tests := []test{
		{
			dst:      []byte{},
			src:      []byte{},
			expected: []byte{},
			shift:    0,
		},
		{
			dst:      []byte{},
			src:      []byte{},
			expected: []byte{},
			shift:    1,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001},
			expected: []byte{0b0000_0010},
			shift:    1,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001},
			expected: []byte{0b0000_0100},
			shift:    2,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001},
			expected: []byte{0b1000_0000},
			shift:    7,
		},
		{
			dst:      make([]byte, 2),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b0000_0011, 0b0000_0010},
			shift:    1,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b0000_0011},
			shift:    1,
		},
		{
			dst:      make([]byte, 2),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b1100_0000, 0b1000_0000},
			shift:    7,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b1100_0000},
			shift:    7,
		},
		{
			dst:      make([]byte, 2),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b1000_0001, 0b0000_0000},
			shift:    8,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b1000_0001},
			shift:    8,
		},
		{
			dst:      make([]byte, 2),
			src:      []byte{0b0000_0001, 0b1000_0001, 0b1111_0101},
			expected: []byte{0b0000_0110, 0b000_0111},
			shift:    2,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("dst: %v, src: %v", tt.dst, tt.src), func(t *testing.T) {
			shiftCopy(tt.dst, string(tt.src), tt.shift)
			require.Equal(t, tt.expected, tt.dst)
		})
	}
}

func initiateValues(batch []database.BatchOp, seed int64) []database.BatchOp {
	r := rand.New(rand.NewSource(seed))
	maxKeyLen := 25
	maxValLen := 32
	for i := 0; i < len(batch); i++ {
		keyLen := r.Intn(maxKeyLen)
		key := make([]byte, keyLen+7)
		_, _ = r.Read(key)

		valueLen := r.Intn(maxValLen)
		value := make([]byte, valueLen)
		_, _ = r.Read(value)
		batch[i] = database.BatchOp{Key: key, Value: value}
	}
	return batch
}

func Test_Insert(t *testing.T) {
	valueToInsert := initiateValues(make([]database.BatchOp, 150000), 0)
	require := require.New(t)
	db, err := newDatabase(
		context.Background(),
		memdb.New(),
		newDefaultConfig(),
		&mockMetrics{},
	)
	require.NoError(err)
	for i := 0; i < 10; i++ {
		view, err := db.NewView(context.Background(), ViewChanges{BatchOps: valueToInsert, ConsumeBytes: true})
		require.NoError(err)
		view.GetMerkleRoot(context.Background())
	}
	require.NoError(db.Close())
}
