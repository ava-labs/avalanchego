// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/log"
)

func testRollup(t *testing.T, longs []uint64, roll int) {
	slice := make([]byte, len(longs)*8)
	numLongs := len(longs)
	for i := 0; i < numLongs; i++ {
		binary.BigEndian.PutUint64(slice[8*i:], longs[i])
	}

	newSlice, err := rollLongWindow(slice, roll)
	if err != nil {
		t.Fatal(err)
	}
	// numCopies is the number of longs that should have been copied over from the previous
	// slice as opposed to being left empty.
	numCopies := numLongs - roll
	for i := 0; i < numLongs; i++ {
		// Extract the long value that is encoded at position [i] in [newSlice]
		num := binary.BigEndian.Uint64(newSlice[8*i:])
		// If the current index is past the point where we should have copied the value
		// over from the previous slice, assert that the value encoded in [newSlice]
		// is 0
		if i >= numCopies {
			if num != 0 {
				t.Errorf("Expected num encoded in newSlice at position %d to be 0, but found %d", i, num)
			}
		} else {
			// Otherwise, check that the value was copied over correctly
			prevIndex := i + roll
			prevNum := longs[prevIndex]
			if prevNum != num {
				t.Errorf("Expected num encoded in new slice at position %d to be %d, but found %d", i, prevNum, num)
			}
		}
	}
}

func TestRollupWindow(t *testing.T) {
	type test struct {
		longs []uint64
		roll  int
	}

	var tests []test = []test{
		{
			[]uint64{1, 2, 3, 4},
			0,
		},
		{
			[]uint64{1, 2, 3, 4},
			1,
		},
		{
			[]uint64{1, 2, 3, 4},
			2,
		},
		{
			[]uint64{1, 2, 3, 4},
			3,
		},
		{
			[]uint64{1, 2, 3, 4},
			4,
		},
		{
			[]uint64{1, 2, 3, 4},
			5,
		},
		{
			[]uint64{121, 232, 432},
			2,
		},
	}

	for _, test := range tests {
		testRollup(t, test.longs, test.roll)
	}
}

type blockDefinition struct {
	timestamp uint64
	gasUsed   uint64
}

type test struct {
	extraData      []byte
	baseFee        *big.Int
	genBlocks      func() []blockDefinition
	minFee, maxFee *big.Int
}

func TestDynamicFees(t *testing.T) {
	spacedTimestamps := []uint64{1, 1, 2, 5, 15, 120}
	var tests []test = []test{
		// Test minimal gas usage
		{
			extraData: nil,
			baseFee:   nil,
			minFee:    big.NewInt(params.ApricotPhase3MinBaseFee),
			maxFee:    big.NewInt(params.ApricotPhase3MaxBaseFee),
			genBlocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   21000,
					})
				}
				return blocks
			},
		},
		// Test overflow handling
		{
			extraData: nil,
			baseFee:   nil,
			minFee:    big.NewInt(params.ApricotPhase3MinBaseFee),
			maxFee:    big.NewInt(params.ApricotPhase3MaxBaseFee),
			genBlocks: func() []blockDefinition {
				blocks := make([]blockDefinition, 0, len(spacedTimestamps))
				for _, timestamp := range spacedTimestamps {
					blocks = append(blocks, blockDefinition{
						timestamp: timestamp,
						gasUsed:   math.MaxUint64,
					})
				}
				return blocks
			},
		},
		{
			extraData: nil,
			baseFee:   nil,
			minFee:    big.NewInt(params.ApricotPhase3MinBaseFee),
			maxFee:    big.NewInt(params.ApricotPhase3MaxBaseFee),
			genBlocks: func() []blockDefinition {
				return []blockDefinition{
					{
						timestamp: 1,
						gasUsed:   1_000_000,
					},
					{
						timestamp: 3,
						gasUsed:   1_000_000,
					},
					{
						timestamp: 5,
						gasUsed:   2_000_000,
					},
					{
						timestamp: 5,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 7,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1000,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1001,
						gasUsed:   6_000_000,
					},
					{
						timestamp: 1002,
						gasUsed:   6_000_000,
					},
				}
			},
		},
	}

	for _, test := range tests {
		testDynamicFeesStaysWithinRange(t, test)
	}
}

func testDynamicFeesStaysWithinRange(t *testing.T, test test) {
	blocks := test.genBlocks()
	initialBlock := blocks[0]
	header := &types.Header{
		Time:    initialBlock.timestamp,
		GasUsed: initialBlock.gasUsed,
		Number:  big.NewInt(0),
		BaseFee: test.baseFee,
		Extra:   test.extraData,
	}

	for index, block := range blocks[1:] {
		nextExtraData, nextBaseFee, err := CalcBaseFee(params.TestChainConfig, header, block.timestamp)
		if err != nil {
			t.Fatalf("Failed to calculate base fee at index %d: %s", index, err)
		}
		if nextBaseFee.Cmp(test.maxFee) > 0 {
			t.Fatalf("Expected fee to stay less than %d, but found %d", test.maxFee, nextBaseFee)
		}
		if nextBaseFee.Cmp(test.minFee) < 0 {
			t.Fatalf("Expected fee to stay greater than %d, but found %d", test.minFee, nextBaseFee)
		}
		log.Info("Update", "baseFee", nextBaseFee)
		header = &types.Header{
			Time:    block.timestamp,
			GasUsed: block.gasUsed,
			Number:  big.NewInt(int64(index) + 1),
			BaseFee: nextBaseFee,
			Extra:   nextExtraData,
		}
	}
}

func TestLongWindow(t *testing.T) {
	longs := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	sumLongs := uint64(0)
	longWindow := make([]byte, 10*8)
	for i, long := range longs {
		sumLongs = sumLongs + long
		binary.BigEndian.PutUint64(longWindow[i*8:], long)
	}

	sum := sumLongWindow(longWindow, 10)
	if sum != sumLongs {
		t.Fatalf("Expected sum to be %d but found %d", sumLongs, sum)
	}

	for i := uint64(0); i < 10; i++ {
		updateLongWindow(longWindow, i*8, i)
		sum = sumLongWindow(longWindow, 10)
		sumLongs += i

		if sum != sumLongs {
			t.Fatalf("Expected sum to be %d but found %d (iteration: %d)", sumLongs, sum, i)
		}
	}
}

func TestLongWindowOverflow(t *testing.T) {
	longs := []uint64{0, 0, 0, 0, 0, 0, 0, 0, 2, math.MaxUint64 - 1}
	longWindow := make([]byte, 10*8)
	for i, long := range longs {
		binary.BigEndian.PutUint64(longWindow[i*8:], long)
	}

	sum := sumLongWindow(longWindow, 10)
	if sum != math.MaxUint64 {
		t.Fatalf("Expected sum to be maxUint64 (%d), but found %d", uint64(math.MaxUint64), sum)
	}

	for i := uint64(0); i < 10; i++ {
		updateLongWindow(longWindow, i*8, i)
		sum = sumLongWindow(longWindow, 10)

		if sum != math.MaxUint64 {
			t.Fatalf("Expected sum to be maxUint64 (%d), but found %d", uint64(math.MaxUint64), sum)
		}
	}
}

func TestSelectBigWithinBounds(t *testing.T) {
	type test struct {
		lower, value, upper, expected *big.Int
	}

	var tests = map[string]test{
		"value within bounds": {
			lower:    big.NewInt(0),
			value:    big.NewInt(5),
			upper:    big.NewInt(10),
			expected: big.NewInt(5),
		},
		"value below lower bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(-1),
			upper:    big.NewInt(10),
			expected: big.NewInt(0),
		},
		"value above upper bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(11),
			upper:    big.NewInt(10),
			expected: big.NewInt(10),
		},
		"value matches lower bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(0),
			upper:    big.NewInt(10),
			expected: big.NewInt(0),
		},
		"value matches upper bound": {
			lower:    big.NewInt(0),
			value:    big.NewInt(10),
			upper:    big.NewInt(10),
			expected: big.NewInt(10),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			v := selectBigWithinBounds(test.lower, test.value, test.upper)
			if v.Cmp(test.expected) != 0 {
				t.Fatalf("Expected (%d), found (%d)", test.expected, v)
			}
		})
	}
}
