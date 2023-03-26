// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

const (
	initialUnaryDescription = "SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [0, 256)"
)

func TestSnowballSingleton(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 2, BetaRogue: 5,
	}
	tree := Tree{}
	tree.Initialize(params, Red)

	require.False(tree.Finalized())

	oneRed := bag.Bag[ids.ID]{}
	oneRed.Add(Red)
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
	oneBlue := bag.Bag[ids.ID]{}
	oneBlue.Add(Blue)
	tree.RecordPoll(oneBlue)
	require.Equal(Red, tree.Preference())
	require.True(tree.Finalized())
}

func TestSnowballRecordUnsuccessfulPoll(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 3, BetaRogue: 5,
	}
	tree := Tree{}
	tree.Initialize(params, Red)

	require.False(tree.Finalized())

	oneRed := bag.Bag[ids.ID]{}
	oneRed.Add(Red)
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
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, Red)
	tree.Add(Blue)

	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	oneBlue := bag.Bag[ids.ID]{}
	oneBlue.Add(Blue)
	require.True(tree.RecordPoll(oneBlue))
	require.Equal(Blue, tree.Preference())
	require.False(tree.Finalized())

	oneRed := bag.Bag[ids.ID]{}
	oneRed.Add(Red)
	require.True(tree.RecordPoll(oneRed))
	require.Equal(Blue, tree.Preference())
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
		K: 1, Alpha: 1, BetaVirtuous: 2, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, zero)
	tree.Add(one)

	// Should do nothing
	tree.Add(one)

	expected := "SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [0, 255)\n" +
		"    SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 255"
	require.Equal(expected, tree.String())
	require.Equal(zero, tree.Preference())
	require.False(tree.Finalized())

	oneBag := bag.Bag[ids.ID]{}
	oneBag.Add(one)
	require.True(tree.RecordPoll(oneBag))
	require.Equal(one, tree.Preference())
	require.False(tree.Finalized())

	expected = "SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [0, 255)\n" +
		"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 255"
	require.Equal(expected, tree.String())

	require.True(tree.RecordPoll(oneBag))
	require.Equal(one, tree.Preference())
	require.True(tree.Finalized())

	expected = "SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 2, SF(Confidence = 2, Finalized = true, SL(Preference = 1))) Bit = 255"
	require.Equal(expected, tree.String())
}

func TestSnowballAddPreviouslyRejected(t *testing.T) {
	require := require.New(t)

	zero := ids.ID{0b00000000}
	one := ids.ID{0b00000001}
	two := ids.ID{0b00000010}
	four := ids.ID{0b00000100}

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, zero)
	tree.Add(one)
	tree.Add(four)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 2)\n" +
			"        SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"            SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"            SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	zeroBag := bag.Bag[ids.ID]{}
	zeroBag.Add(zero)
	require.True(tree.RecordPoll(zeroBag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(two)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}
}

func TestSnowballNewUnary(t *testing.T) {
	require := require.New(t)

	zero := ids.ID{0b00000000}
	one := ids.ID{0b00000001}

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 2, BetaRogue: 3,
	}
	tree := Tree{}
	tree.Initialize(params, zero)
	tree.Add(one)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	oneBag := bag.Bag[ids.ID]{}
	oneBag.Add(one)
	require.True(tree.RecordPoll(oneBag))

	{
		expected := "SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(one, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(oneBag))

	{
		expected := "SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 2, SF(Confidence = 2, Finalized = false, SL(Preference = 1))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 2, SF(Confidence = 2, Finalized = true)) Bits = [1, 256)"
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
		K: 1, Alpha: 1, BetaVirtuous: 2, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, zero)
	tree.Add(two)
	tree.Add(eight)

	{
		expected := "SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [0, 1)\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 3)\n" +
			"            SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 3\n" +
			"                SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [4, 256)\n" +
			"                SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [4, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	zeroBag := bag.Bag[ids.ID]{}
	zeroBag.Add(zero)
	require.True(tree.RecordPoll(zeroBag))

	{
		expected := "SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [0, 1)\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [2, 3)\n" +
			"            SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 3\n" +
			"                SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [4, 256)\n" +
			"                SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [4, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	emptyBag := bag.Bag[ids.ID]{}
	require.False(tree.RecordPoll(emptyBag))

	{
		expected := "SB(NumSuccessfulPolls = 1, SF(Confidence = 0, Finalized = false)) Bits = [0, 1)\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [2, 3)\n" +
			"            SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 3\n" +
			"                SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [4, 256)\n" +
			"                SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [4, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(zeroBag))

	{
		expected := "SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = false)) Bits = [0, 1)\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = false)) Bits = [2, 3)\n" +
			"            SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 3\n" +
			"                SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = false)) Bits = [4, 256)\n" +
			"                SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [4, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(zeroBag))

	{
		expected := "SB(NumSuccessfulPolls = 3, SF(Confidence = 2, Finalized = true)) Bits = [4, 256)"
		require.Equal(expected, tree.String())
		require.Equal(zero, tree.Preference())
		require.True(tree.Finalized())
	}
}

func TestSnowballTrinary(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, Green)
	tree.Add(Red)
	tree.Add(Blue)

	//       *
	//      / \
	//     R   *
	//        / \
	//       G   B

	require.Equal(Green, tree.Preference())
	require.False(tree.Finalized())

	redBag := bag.Bag[ids.ID]{}
	redBag.Add(Red)
	require.True(tree.RecordPoll(redBag))
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	blueBag := bag.Bag[ids.ID]{}
	blueBag.Add(Blue)
	require.True(tree.RecordPoll(blueBag))
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())

	// Here is a case where voting for a color makes a different color become
	// the preferred color. This is intended behavior.
	greenBag := bag.Bag[ids.ID]{}
	greenBag.Add(Green)
	require.True(tree.RecordPoll(greenBag))
	require.Equal(Blue, tree.Preference())
	require.False(tree.Finalized())

	// Red has already been rejected here, so this is not a successful poll.
	require.False(tree.RecordPoll(redBag))
	require.Equal(Blue, tree.Preference())
	require.False(tree.Finalized())

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
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, yellow)
	tree.Add(cyan)
	tree.Add(magenta)

	//       *
	//      / \
	//     C   *
	//        / \
	//       Y   M

	require.Equal(yellow, tree.Preference())
	require.False(tree.Finalized())

	yellowBag := bag.Bag[ids.ID]{}
	yellowBag.Add(yellow)
	require.True(tree.RecordPoll(yellowBag))
	require.Equal(yellow, tree.Preference())
	require.False(tree.Finalized())

	magentaBag := bag.Bag[ids.ID]{}
	magentaBag.Add(magenta)
	require.True(tree.RecordPoll(magentaBag))
	require.Equal(yellow, tree.Preference())
	require.False(tree.Finalized())

	// Cyan has already been rejected here, so these are not successful polls.
	cyanBag := bag.Bag[ids.ID]{}
	cyanBag.Add(cyan)
	require.False(tree.RecordPoll(cyanBag))
	require.Equal(yellow, tree.Preference())
	require.False(tree.Finalized())

	require.False(tree.RecordPoll(cyanBag))
	require.Equal(yellow, tree.Preference())
	require.False(tree.Finalized())
}

func TestSnowballAddRejected(t *testing.T) {
	require := require.New(t)

	c0000 := ids.ID{0x00} // 0000
	c1000 := ids.ID{0x01} // 1000
	c0101 := ids.ID{0x0a} // 0101
	c0010 := ids.ID{0x04} // 0010

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, c0000)
	tree.Add(c1000)
	tree.Add(c0010)

	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	c0010Bag := bag.Bag[ids.ID]{}
	c0010Bag.Add(c0010)
	require.True(tree.RecordPoll(c0010Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0010, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c0101)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0010, tree.Preference())
		require.False(tree.Finalized())
	}
}

func TestSnowballResetChild(t *testing.T) {
	require := require.New(t)

	c0000 := ids.ID{0x00} // 0000
	c0100 := ids.ID{0x02} // 0100
	c1000 := ids.ID{0x01} // 1000

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, c0000)
	tree.Add(c0100)
	tree.Add(c1000)

	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	c0000Bag := bag.Bag[ids.ID]{}
	c0000Bag.Add(c0000)
	require.True(tree.RecordPoll(c0000Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	emptyBag := bag.Bag[ids.ID]{}
	require.False(tree.RecordPoll(emptyBag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(c0000Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
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
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, c0000)
	tree.Add(c0100)
	tree.Add(c1000)

	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	c0100Bag := bag.Bag[ids.ID]{}
	c0100Bag.Add(c0100)
	require.True(tree.RecordPoll(c0100Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0100, tree.Preference())
		require.False(tree.Finalized())
	}

	c1000Bag := bag.Bag[ids.ID]{}
	c1000Bag.Add(c1000)
	require.True(tree.RecordPoll(c1000Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0100, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(c0100Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 2, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0100, tree.Preference())
		require.False(tree.Finalized())
	}
}

func TestSnowball5Colors(t *testing.T) {
	require := require.New(t)

	numColors := 5
	params := Parameters{
		K: 5, Alpha: 5, BetaVirtuous: 20, BetaRogue: 30,
	}

	colors := []ids.ID{}
	for i := 0; i < numColors; i++ {
		colors = append(colors, ids.Empty.Prefix(uint64(i)))
	}

	tree0 := Tree{}
	tree0.Initialize(params, colors[4])

	tree0.Add(colors[0])
	tree0.Add(colors[1])
	tree0.Add(colors[2])
	tree0.Add(colors[3])

	tree1 := Tree{}
	tree1.Initialize(params, colors[3])

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
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, c0000)

	require.Equal(initialUnaryDescription, tree.String())
	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	tree.Add(c1100)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c1000)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c0010)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 2)\n" +
			"        SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"            SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"            SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	c0000Bag := bag.Bag[ids.ID]{}
	c0000Bag.Add(c0000)
	require.True(tree.RecordPoll(c0000Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	c0010Bag := bag.Bag[ids.ID]{}
	c0010Bag.Add(c0010)
	require.True(tree.RecordPoll(c0010Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 2\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	require.True(tree.RecordPoll(c0010Bag))

	{
		expected := "SB(NumSuccessfulPolls = 2, SF(Confidence = 2, Finalized = true)) Bits = [3, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0010, tree.Preference())
		require.True(tree.Finalized())
	}
}

func TestSnowballDoubleAdd(t *testing.T) {
	require := require.New(t)

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 3, BetaRogue: 5,
	}
	tree := Tree{}
	tree.Initialize(params, Red)
	tree.Add(Red)

	require.Equal(initialUnaryDescription, tree.String())
	require.Equal(Red, tree.Preference())
	require.False(tree.Finalized())
}

func TestSnowballConsistent(t *testing.T) {
	require := require.New(t)

	numColors := 50
	numNodes := 100
	params := Parameters{
		K: 20, Alpha: 15, BetaVirtuous: 20, BetaRogue: 30,
	}
	seed := int64(0)

	sampler.Seed(seed)

	n := Network{}
	n.Initialize(params, numColors)

	for i := 0; i < numNodes; i++ {
		n.AddNode(&Tree{})
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
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, c0000)

	require.Equal(initialUnaryDescription, tree.String())
	require.Equal(c0000, tree.Preference())
	require.False(tree.Finalized())

	tree.Add(c1000)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c0010)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 2)\n" +
			"        SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"            SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"            SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	c0000Bag := bag.Bag[ids.ID]{}
	c0000Bag.Add(c0000)
	require.True(tree.RecordPoll(c0000Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	tree.Add(c0100)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}

	c0100Bag := bag.Bag[ids.ID]{}
	c0100Bag.Add(c0100)
	require.True(tree.RecordPoll(c0100Bag))

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)"
		require.Equal(expected, tree.String())
		require.Equal(c0000, tree.Preference())
		require.False(tree.Finalized())
	}
}
