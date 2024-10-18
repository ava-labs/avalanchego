// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//nolint:goconst
package snowball

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/mathext/prng"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

const initialUnaryDescription = "SF(Confidence = [0], Finalized = false) Bits = [0, 256)"

func TestSnowballSingleton(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, Red)

	require.False(tree.Finalized())

	oneRed := bag.Of(Red)
	require.True(tree.RecordPoll(oneRed))
	require.False(tree.Finalized())

	empty := bag.Bag[ids.ID]{}
	require.False(tree.RecordPoll(empty))
	require.False(tree.Finalized())

	require.True(tree.RecordPoll(oneRed))
	require.False(tree.Finalized())

	require.True(tree.RecordPoll(oneRed))
	require.Equal(Red, tree.Preference())
	require.True(tree.Finalized())

	tree.Add(Blue)

	require.True(tree.Finalized())

	// Because the tree is already finalized, RecordPoll can return either true
	// or false.
	oneBlue := bag.Of(Blue)
	tree.RecordPoll(oneBlue)
	require.Equal(Red, tree.Preference())
	require.True(tree.Finalized())
}

func TestSnowballRecordUnsuccessfulPoll(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            3,
	}
	tree := NewTree(SnowflakeFactory, params, Red)

	require.False(tree.Finalized())

	oneRed := bag.Of(Red)
	require.True(tree.RecordPoll(oneRed))

	tree.RecordUnsuccessfulPoll()

	require.True(tree.RecordPoll(oneRed))
	require.False(tree.Finalized())

	require.True(tree.RecordPoll(oneRed))
	require.False(tree.Finalized())

	require.True(tree.RecordPoll(oneRed))
	require.Equal(Red, tree.Preference())
	require.True(tree.Finalized())
}

func TestSnowballBinary(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, Red)
	tree.Add(Blue)

	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	oneBlue := bag.Of(Blue)
	require.True(tree.RecordPoll(oneBlue))
	require.Equal(Blue, tree.Preference())
	require.False(tree.Finalized())

	oneRed := bag.Of(Red)
	require.True(tree.RecordPoll(oneRed))
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	require.True(tree.RecordPoll(oneBlue))
	require.Equal(Blue, tree.Preference())
	require.False(tree.Finalized())

	require.True(tree.RecordPoll(oneBlue))
	require.Equal(Blue, tree.Preference())
	require.True(tree.Finalized())
}

func TestSnowballLastBinary(t *testing.T) {
	require := require.New(t)

	zero := ids.Empty
	one := ids.ID{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80,
	}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, zero)
	tree.Add(one)

	// Should do nothing
	tree.Add(one)

	expected := `SF(Confidence = [0], Finalized = false) Bits = [0, 255)
    SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 255`
	require.Equal(expected, tree.String())
	require.Equal(zero, tree.Preference())
	require.False(tree.Finalized())

	oneBag := bag.Of(one)
	require.True(tree.RecordPoll(oneBag))
	require.Equal(one, tree.Preference())
	require.False(tree.Finalized())

	expected = `SF(Confidence = [1], Finalized = false) Bits = [0, 255)
    SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 255`
	require.Equal(expected, tree.String())

	require.True(tree.RecordPoll(oneBag))
	require.Equal(one, tree.Preference())
	require.True(tree.Finalized())

	expected = "SF(Confidence = [2], Finalized = true, SL(Preference = 1)) Bit = 255"
	require.Equal(expected, tree.String())
}

func TestSnowballFirstBinary(t *testing.T) {
	require := require.New(t)

	zero := ids.Empty
	one := ids.ID{0x01}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, zero)
	tree.Add(one)

	expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
	require.Equal(expected, tree.String())
	require.Equal(zero, tree.Preference())
	require.False(tree.Finalized())

	oneBag := bag.Of(one)
	require.True(tree.RecordPoll(oneBag))
	require.Equal(one, tree.Preference())
	require.False(tree.Finalized())

	expected = `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [1], Finalized = false) Bits = [1, 256)`
	require.Equal(expected, tree.String())

	require.True(tree.RecordPoll(oneBag))
	require.Equal(one, tree.Preference())
	require.True(tree.Finalized())

	expected = `SF(Confidence = [2], Finalized = true) Bits = [1, 256)`
	require.Equal(expected, tree.String())
}

func TestSnowballAddDecidedFirstBit(t *testing.T) {
	require := require.New(t)

	zero := ids.Empty
	c1000 := ids.ID{0x01}
	c1100 := ids.ID{0x03}
	c0110 := ids.ID{0x06}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, zero)
	tree.Add(c1000)
	tree.Add(c1100)

	expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
	require.Equal(expected, tree.String())
	require.Equal(zero, tree.Preference())
	require.False(tree.Finalized())

	oneBag := bag.Of(c1000)
	require.True(tree.RecordPoll(oneBag))
	require.Equal(c1000, tree.Preference())
	require.False(tree.Finalized())

	expected = `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
	require.Equal(expected, tree.String())

	threeBag := bag.Of(c1100)
	require.True(tree.RecordPoll(threeBag))
	require.Equal(c1100, tree.Preference())
	require.False(tree.Finalized())

	expected = `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 1
    SF(Confidence = [1], Finalized = false) Bits = [2, 256)
    SF(Confidence = [1], Finalized = false) Bits = [2, 256)`
	require.Equal(expected, tree.String())

	// Adding six should have no effect because the first bit is already decided
	tree.Add(c0110)
	require.Equal(expected, tree.String())
}

func TestSnowballAddPreviouslyRejected(t *testing.T) {
	require := require.New(t)

	zero := ids.ID{0b00000000}
	one := ids.ID{0b00000001}
	two := ids.ID{0b00000010}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, zero)
	tree.Add(two)

	{
		expected := `SF(Confidence = [0], Finalized = false) Bits = [0, 1)
    SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	zeroBag := bag.Of(zero)
	require.True(tree.RecordPoll(zeroBag))

	{
		expected := `SF(Confidence = [1], Finalized = false) Bits = [0, 1)
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	twoBag := bag.Of(two)
	require.True(tree.RecordPoll(twoBag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 1
    SF(Confidence = [1], Finalized = false) Bits = [2, 256)
    SF(Confidence = [1], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(two, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(one)

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 1
    SF(Confidence = [1], Finalized = false) Bits = [2, 256)
    SF(Confidence = [1], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(two, tree.Preference())
		require.False(tree.Finalized())
	}
}

func TestSnowballNewUnary(t *testing.T) {
	require := require.New(t)

	zero := ids.ID{0b00000000}
	one := ids.ID{0b00000001}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            3,
	}
	tree := NewTree(SnowflakeFactory, params, zero)
	tree.Add(one)

	{
		expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	oneBag := bag.Of(one)
	require.True(tree.RecordPoll(oneBag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [1], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(one, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(oneBag))

	{
		expected := `SF(Confidence = [2], Finalized = false, SL(Preference = 1)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [2], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(one, tree.Preference())
		require.False(tree.Finalized())
	}
}

func TestSnowballTransitiveReset(t *testing.T) {
	require := require.New(t)

	zero := ids.ID{0b00000000}
	two := ids.ID{0b00000010}
	eight := ids.ID{0b00001000}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, zero)
	tree.Add(two)
	tree.Add(eight)

	{
		expected := `SF(Confidence = [0], Finalized = false) Bits = [0, 1)
    SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 3)
            SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 3
                SF(Confidence = [0], Finalized = false) Bits = [4, 256)
                SF(Confidence = [0], Finalized = false) Bits = [4, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	zeroBag := bag.Of(zero)
	require.True(tree.RecordPoll(zeroBag))

	{
		expected := `SF(Confidence = [1], Finalized = false) Bits = [0, 1)
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false) Bits = [2, 3)
            SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 3
                SF(Confidence = [1], Finalized = false) Bits = [4, 256)
                SF(Confidence = [0], Finalized = false) Bits = [4, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	emptyBag := bag.Bag[ids.ID]{}
	require.False(tree.RecordPoll(emptyBag))

	{
		expected := `SF(Confidence = [0], Finalized = false) Bits = [0, 1)
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false) Bits = [2, 3)
            SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 3
                SF(Confidence = [1], Finalized = false) Bits = [4, 256)
                SF(Confidence = [0], Finalized = false) Bits = [4, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(zeroBag))

	{
		expected := `SF(Confidence = [1], Finalized = false) Bits = [0, 1)
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false) Bits = [2, 3)
            SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 3
                SF(Confidence = [1], Finalized = false) Bits = [4, 256)
                SF(Confidence = [0], Finalized = false) Bits = [4, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(zeroBag))

	{
		expected := "SF(Confidence = [2], Finalized = true) Bits = [4, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.True(tree.Finalized())
	}
}

func TestSnowballTrinary(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, Green)
	tree.Add(Red)
	tree.Add(Blue)

	//       *
	//      / \
	//     R   *
	//        / \
	//       G   B

	require.Equal(Green, tree.Preference())
	require.False(tree.Finalized())

	redBag := bag.Of(Red)
	require.True(tree.RecordPoll(redBag))
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	//       *
	//     1/ \
	//     R   *
	//        / \
	//       G   B

	blueBag := bag.Of(Blue)
	require.True(tree.RecordPoll(blueBag))
	require.Equal(Blue, tree.Preference())
	require.False(tree.Finalized())

	//       *
	//     1/ \1
	//     R   *
	//        / \1
	//       G   B

	greenBag := bag.Of(Green)
	require.True(tree.RecordPoll(greenBag))
	require.Equal(Green, tree.Preference())
	require.False(tree.Finalized())

	//       *
	//     1/ \2
	//     R   *
	//       1/ \1
	//       G   B

	// Red has already been rejected here, so this is not a successful poll.
	require.False(tree.RecordPoll(redBag))
	require.Equal(Green, tree.Preference())
	require.False(tree.Finalized())

	//       *
	//     1/ \3
	//     R   *
	//       2/ \1
	//       G   B
	require.True(tree.RecordPoll(greenBag))
	require.Equal(Green, tree.Preference())
	require.False(tree.Finalized())
}

func TestSnowballCloseTrinary(t *testing.T) {
	require := require.New(t)

	yellow := ids.ID{0x01}
	cyan := ids.ID{0x02}
	magenta := ids.ID{0x03}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, yellow)
	tree.Add(cyan)
	tree.Add(magenta)

	//       *
	//      / \
	//     C   *
	//        / \
	//       Y   M

	require.Equal(yellow, tree.Preference())
	require.False(tree.Finalized())

	yellowBag := bag.Of(yellow)
	require.True(tree.RecordPoll(yellowBag))
	require.Equal(yellow, tree.Preference())
	require.False(tree.Finalized())

	//       *
	//      / \1
	//     C   *
	//       1/ \
	//       Y   M

	magentaBag := bag.Of(magenta)
	require.True(tree.RecordPoll(magentaBag))
	require.Equal(magenta, tree.Preference())
	require.False(tree.Finalized())

	//       *
	//      / \2
	//     C   *
	//       1/ \1
	//       Y   M

	// Cyan has already been rejected here, so these are not successful polls.
	cyanBag := bag.Of(cyan)
	require.False(tree.RecordPoll(cyanBag))
	require.Equal(magenta, tree.Preference())
	require.False(tree.Finalized())

	//       *
	//      / \2
	//     C   *
	//       1/ \1
	//       Y   M

	require.False(tree.RecordPoll(cyanBag))
	require.Equal(magenta, tree.Preference())
	require.False(tree.Finalized())
}

func TestSnowballResetChild(t *testing.T) {
	require := require.New(t)

	c0000 := ids.ID{0x00} // 0000
	c0100 := ids.ID{0x02} // 0100
	c1000 := ids.ID{0x01} // 1000

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, c0000)
	tree.Add(c0100)
	tree.Add(c1000)

	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	c0000Bag := bag.Of(c0000)
	require.True(tree.RecordPoll(c0000Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	emptyBag := bag.Bag[ids.ID]{}
	require.False(tree.RecordPoll(emptyBag))

	{
		expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(c0000Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}
}

func TestSnowballResetSibling(t *testing.T) {
	require := require.New(t)

	c0000 := ids.ID{0x00} // 0000
	c0100 := ids.ID{0x02} // 0100
	c1000 := ids.ID{0x01} // 1000

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, c0000)
	tree.Add(c0100)
	tree.Add(c1000)

	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	c0100Bag := bag.Of(c0100)
	require.True(tree.RecordPoll(c0100Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
        SF(Confidence = [1], Finalized = false) Bits = [2, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0100, tree.Preference())
		require.False(tree.Finalized())
	}

	c1000Bag := bag.Of(c1000)
	require.True(tree.RecordPoll(c1000Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 0
    SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
        SF(Confidence = [1], Finalized = false) Bits = [2, 256)
    SF(Confidence = [1], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c1000, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(c0100Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
        SF(Confidence = [1], Finalized = false) Bits = [2, 256)
    SF(Confidence = [1], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0100, tree.Preference())
		require.False(tree.Finalized())
	}
}

func TestSnowball5Colors(t *testing.T) {
	require := require.New(t)

	numColors := 5
	params := Parameters{
		K:               5,
		AlphaPreference: 5,
		AlphaConfidence: 5,
		Beta:            20,
	}

	colors := []ids.ID{}
	for i := 0; i < numColors; i++ {
		colors = append(colors, ids.Empty.Prefix(uint64(i)))
	}

	tree0 := NewTree(SnowflakeFactory, params, colors[4])

	tree0.Add(colors[0])
	tree0.Add(colors[1])
	tree0.Add(colors[2])
	tree0.Add(colors[3])

	tree1 := NewTree(SnowflakeFactory, params, colors[3])

	tree1.Add(colors[0])
	tree1.Add(colors[1])
	tree1.Add(colors[2])
	tree1.Add(colors[4])

	s1 := tree0.String()
	s2 := tree1.String()
	require.Equal(strings.Count(s1, "    "), strings.Count(s2, "    "))
}

func TestSnowballFineGrained(t *testing.T) {
	require := require.New(t)

	c0000 := ids.ID{0x00}
	c1000 := ids.ID{0x01}
	c1100 := ids.ID{0x03}
	c0010 := ids.ID{0x04}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, c0000)

	require.Equal(initialUnaryDescription, tree.String())
	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	tree.Add(c1100)

	{
		expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c1000)

	{
		expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [0], Finalized = false, SL(Preference = 1)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c0010)

	{
		expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 2)
        SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 2
            SF(Confidence = [0], Finalized = false) Bits = [3, 256)
            SF(Confidence = [0], Finalized = false) Bits = [3, 256)
    SF(Confidence = [0], Finalized = false, SL(Preference = 1)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	c0000Bag := bag.Of(c0000)
	require.True(tree.RecordPoll(c0000Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [1], Finalized = false) Bits = [1, 2)
        SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 2
            SF(Confidence = [1], Finalized = false) Bits = [3, 256)
            SF(Confidence = [0], Finalized = false) Bits = [3, 256)
    SF(Confidence = [0], Finalized = false, SL(Preference = 1)) Bit = 1
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	c0010Bag := bag.Of(c0010)
	require.True(tree.RecordPoll(c0010Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 2
    SF(Confidence = [1], Finalized = false) Bits = [3, 256)
    SF(Confidence = [1], Finalized = false) Bits = [3, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0010, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(c0010Bag))
	{
		expected := "SF(Confidence = [2], Finalized = true) Bits = [3, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0010, tree.Preference())
		require.True(tree.Finalized())
	}
}

func TestSnowballDoubleAdd(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            3,
	}
	tree := NewTree(SnowflakeFactory, params, Red)
	tree.Add(Red)

	require.Equal(initialUnaryDescription, tree.String())
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())
}

func TestSnowballConsistent(t *testing.T) {
	require := require.New(t)

	var (
		numColors = 50
		numNodes  = 100
		params    = Parameters{
			K:               20,
			AlphaPreference: 15,
			AlphaConfidence: 15,
			Beta:            20,
		}
		seed   uint64 = 0
		source        = prng.NewMT19937()
	)

	n := NewNetwork(SnowflakeFactory, params, numColors, source)

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		n.AddNode(NewTree)
	}

	for !n.Finalized() && !n.Disagreement() {
		n.Round()
	}

	require.True(n.Agreement())
}

func TestSnowballFilterBinaryChildren(t *testing.T) {
	require := require.New(t)

	c0000 := ids.ID{0b00000000}
	c1000 := ids.ID{0b00000001}
	c0100 := ids.ID{0b00000010}
	c0010 := ids.ID{0b00000100}

	params := Parameters{
		K:               1,
		AlphaPreference: 1,
		AlphaConfidence: 1,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, c0000)

	require.Equal(initialUnaryDescription, tree.String())
	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	tree.Add(c1000)

	{
		expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c0010)

	{
		expected := `SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [0], Finalized = false) Bits = [1, 2)
        SF(Confidence = [0], Finalized = false, SL(Preference = 0)) Bit = 2
            SF(Confidence = [0], Finalized = false) Bits = [3, 256)
            SF(Confidence = [0], Finalized = false) Bits = [3, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	c0000Bag := bag.Of(c0000)
	require.True(tree.RecordPoll(c0000Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [1], Finalized = false) Bits = [1, 2)
        SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 2
            SF(Confidence = [1], Finalized = false) Bits = [3, 256)
            SF(Confidence = [0], Finalized = false) Bits = [3, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c0100)

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 0
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 1
        SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 2
            SF(Confidence = [1], Finalized = false) Bits = [3, 256)
            SF(Confidence = [0], Finalized = false) Bits = [3, 256)
        SF(Confidence = [0], Finalized = false) Bits = [2, 256)
    SF(Confidence = [0], Finalized = false) Bits = [1, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	c0100Bag := bag.Of(c0100)
	require.True(tree.RecordPoll(c0100Bag))

	{
		expected := `SF(Confidence = [1], Finalized = false, SL(Preference = 1)) Bit = 1
    SF(Confidence = [1], Finalized = false, SL(Preference = 0)) Bit = 2
        SF(Confidence = [1], Finalized = false) Bits = [3, 256)
        SF(Confidence = [0], Finalized = false) Bits = [3, 256)
    SF(Confidence = [1], Finalized = false) Bits = [2, 256)`
		require.Equal(expected, tree.String())
		require.Equal(c0100, tree.Preference())
		require.False(tree.Finalized())
	}
}

func TestSnowballRecordPreferencePollBinary(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K:               3,
		AlphaPreference: 2,
		AlphaConfidence: 3,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, Red)
	tree.Add(Blue)
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	threeBlue := bag.Of(Blue, Blue, Blue)
	require.True(tree.RecordPoll(threeBlue))
	require.Equal(Blue, tree.Preference())
	require.False(tree.Finalized())

	twoRed := bag.Of(Red, Red)
	require.True(tree.RecordPoll(twoRed))
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	threeRed := bag.Of(Red, Red, Red)
	require.True(tree.RecordPoll(threeRed))
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	require.True(tree.RecordPoll(threeRed))
	require.Equal(Red, tree.Preference())
	require.True(tree.Finalized())

	require.False(tree.RecordPoll(threeBlue))
	require.Equal(Red, tree.Preference())
	require.True(tree.Finalized())
}

func TestSnowballRecordPreferencePollUnary(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K:               3,
		AlphaPreference: 2,
		AlphaConfidence: 3,
		Beta:            2,
	}
	tree := NewTree(SnowflakeFactory, params, Red)
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	twoRed := bag.Of(Red, Red)
	require.True(tree.RecordPoll(twoRed))
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	tree.Add(Blue)

	threeBlue := bag.Of(Blue, Blue, Blue)
	require.True(tree.RecordPoll(threeBlue))
	require.Equal(Blue, tree.Preference())
	require.False(tree.Finalized())

	require.True(tree.RecordPoll(threeBlue))
	require.Equal(Blue, tree.Preference())
	require.True(tree.Finalized())
}
