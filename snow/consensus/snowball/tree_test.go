// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"strings"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

const (
	initialUnaryDescription = "SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [0, 256)"
)

func TestTreeParams(t *testing.T) { ParamsTest(t, TreeFactory{}) }

func TestSnowballSingleton(t *testing.T) {
	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 2, BetaRogue: 5,
	}
	tree := Tree{}
	tree.Initialize(params, Red)

	if tree.Finalized() {
		t.Fatalf("Snowball is finalized too soon")
	}

	oneRed := ids.Bag{}
	oneRed.Add(Red)
	tree.RecordPoll(oneRed)

	if tree.Finalized() {
		t.Fatalf("Snowball is finalized too soon")
	}

	empty := ids.Bag{}
	tree.RecordPoll(empty)

	if tree.Finalized() {
		t.Fatalf("Snowball is finalized too soon")
	}

	tree.RecordPoll(oneRed)

	if tree.Finalized() {
		t.Fatalf("Snowball is finalized too soon")
	}

	tree.RecordPoll(oneRed)

	if !tree.Finalized() {
		t.Fatalf("Snowball should be finalized")
	} else if Red != tree.Preference() {
		t.Fatalf("After only voting red, something else was decided")
	}

	tree.Add(Blue)

	oneBlue := ids.Bag{}
	oneBlue.Add(Blue)
	tree.RecordPoll(oneBlue)

	if !tree.Finalized() {
		t.Fatalf("Snowball should be finalized")
	} else if Red != tree.Preference() {
		t.Fatalf("After only voting red, something else was decided")
	}
}

func TestSnowballRecordUnsuccessfulPoll(t *testing.T) {
	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 3, BetaRogue: 5,
	}
	tree := Tree{}
	tree.Initialize(params, Red)

	if tree.Finalized() {
		t.Fatalf("Snowball is finalized too soon")
	}

	oneRed := ids.Bag{}
	oneRed.Add(Red)
	tree.RecordPoll(oneRed)

	tree.RecordUnsuccessfulPoll()

	tree.RecordPoll(oneRed)

	if tree.Finalized() {
		t.Fatalf("Snowball is finalized too soon")
	}

	tree.RecordPoll(oneRed)

	if tree.Finalized() {
		t.Fatalf("Snowball is finalized too soon")
	}

	tree.RecordPoll(oneRed)

	if !tree.Finalized() {
		t.Fatalf("Snowball should be finalized")
	} else if Red != tree.Preference() {
		t.Fatalf("After only voting red, something else was decided")
	}
}

func TestSnowballBinary(t *testing.T) {
	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, Red)
	tree.Add(Blue)

	if pref := tree.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	oneBlue := ids.Bag{}
	oneBlue.Add(Blue)
	tree.RecordPoll(oneBlue)

	if pref := tree.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	oneRed := ids.Bag{}
	oneRed.Add(Red)
	tree.RecordPoll(oneRed)

	if pref := tree.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	tree.RecordPoll(oneBlue)

	if pref := tree.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	tree.RecordPoll(oneBlue)

	if pref := tree.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if !tree.Finalized() {
		t.Fatalf("Didn't finalized correctly")
	}
}

func TestSnowballLastBinary(t *testing.T) {
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
	if str := tree.String(); expected != str {
		t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
	} else if pref := tree.Preference(); zero != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	oneBag := ids.Bag{}
	oneBag.Add(one)
	tree.RecordPoll(oneBag)

	if pref := tree.Preference(); one != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", one, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	tree.RecordPoll(oneBag)

	if pref := tree.Preference(); one != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", one, pref)
	} else if !tree.Finalized() {
		t.Fatalf("Finalized too late")
	}
}

func TestSnowballAddPreviouslyRejected(t *testing.T) {
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
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	zeroBag := ids.Bag{}
	zeroBag.Add(zero)
	tree.RecordPoll(zeroBag)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.Add(two)

	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}
}

func TestSnowballNewUnary(t *testing.T) {
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
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	oneBag := ids.Bag{}
	oneBag.Add(one)
	tree.RecordPoll(oneBag)

	{
		expected := "SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); one != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", one, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.RecordPoll(oneBag)

	{
		expected := "SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 2, SF(Confidence = 2, Finalized = false, SL(Preference = 1))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 2, SF(Confidence = 2, Finalized = true)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); one != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", one, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}
}

func TestSnowballTransitiveReset(t *testing.T) {
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
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	zeroBag := ids.Bag{}
	zeroBag.Add(zero)
	tree.RecordPoll(zeroBag)

	{
		expected := "SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [0, 1)\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [2, 3)\n" +
			"            SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 3\n" +
			"                SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [4, 256)\n" +
			"                SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [4, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	emptyBag := ids.Bag{}
	tree.RecordPoll(emptyBag)

	{
		expected := "SB(NumSuccessfulPolls = 1, SF(Confidence = 0, Finalized = false)) Bits = [0, 1)\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [2, 3)\n" +
			"            SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 3\n" +
			"                SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = false)) Bits = [4, 256)\n" +
			"                SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [4, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.RecordPoll(zeroBag)

	{
		expected := "SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = false)) Bits = [0, 1)\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = false)) Bits = [2, 3)\n" +
			"            SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 3\n" +
			"                SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = false)) Bits = [4, 256)\n" +
			"                SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [4, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.RecordPoll(zeroBag)

	{
		expected := "SB(NumSuccessfulPolls = 3, SF(Confidence = 2, Finalized = true)) Bits = [4, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); zero != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", zero, pref)
		} else if !tree.Finalized() {
			t.Fatalf("Finalized too late")
		}
	}
}

func TestSnowballTrinary(t *testing.T) {
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

	if pref := tree.Preference(); Green != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Green, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	redBag := ids.Bag{}
	redBag.Add(Red)
	tree.RecordPoll(redBag)

	if pref := tree.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	blueBag := ids.Bag{}
	blueBag.Add(Blue)
	tree.RecordPoll(blueBag)

	if pref := tree.Preference(); Red != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	greenBag := ids.Bag{}
	greenBag.Add(Green)
	tree.RecordPoll(greenBag)

	// Here is a case where voting for a color makes a different color become
	// the preferred color. This is intended behavior.
	if pref := tree.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	tree.RecordPoll(redBag)

	if pref := tree.Preference(); Blue != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Blue, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	tree.RecordPoll(greenBag)

	if pref := tree.Preference(); Green != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", Green, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}
}

func TestSnowballCloseTrinary(t *testing.T) {
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

	if pref := tree.Preference(); yellow != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", yellow, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	yellowBag := ids.Bag{}
	yellowBag.Add(yellow)
	tree.RecordPoll(yellowBag)

	if pref := tree.Preference(); yellow != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", yellow, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	magentaBag := ids.Bag{}
	magentaBag.Add(magenta)
	tree.RecordPoll(magentaBag)

	if pref := tree.Preference(); yellow != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", yellow, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	cyanBag := ids.Bag{}
	cyanBag.Add(cyan)
	tree.RecordPoll(cyanBag)

	if pref := tree.Preference(); yellow != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", yellow, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	tree.RecordPoll(cyanBag)

	if pref := tree.Preference(); yellow != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", yellow, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}
}

func TestSnowballAddRejected(t *testing.T) {
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

	if pref := tree.Preference(); c0000 != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	c0010Bag := ids.Bag{}
	c0010Bag.Add(c0010)

	tree.RecordPoll(c0010Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0010 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0010, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.Add(c0101)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0010 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0010, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}
}

func TestSnowballResetChild(t *testing.T) {
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

	if pref := tree.Preference(); c0000 != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	c0000Bag := ids.Bag{}
	c0000Bag.Add(c0000)

	tree.RecordPoll(c0000Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	emptyBag := ids.Bag{}

	tree.RecordPoll(emptyBag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.RecordPoll(c0000Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}
}

func TestSnowballResetSibling(t *testing.T) {
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

	if pref := tree.Preference(); c0000 != pref {
		t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
	} else if tree.Finalized() {
		t.Fatalf("Finalized too early")
	}

	c0100Bag := ids.Bag{}
	c0100Bag.Add(c0100)

	tree.RecordPoll(c0100Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0100 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0100, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	c1000Bag := ids.Bag{}
	c1000Bag.Add(c1000)

	tree.RecordPoll(c1000Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0100 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0100, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.RecordPoll(c0100Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 2, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = true)) Bits = [2, 256)\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0100 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0100, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}
}

func TestSnowball5Colors(t *testing.T) {
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
	if strings.Count(s1, "    ") != strings.Count(s2, "    ") {
		t.Fatalf("Mis-matched initial values:\n\n%s\n\n%s",
			s1, s2)
	}
}

func TestSnowballFineGrained(t *testing.T) {
	c0000 := ids.ID{0x00}
	c1000 := ids.ID{0x01}
	c1100 := ids.ID{0x03}
	c0010 := ids.ID{0x04}

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, c0000)
	{
		expected := initialUnaryDescription
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.Add(c1100)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.Add(c1000)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
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
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	c0000Bag := ids.Bag{}
	c0000Bag.Add(c0000)
	tree.RecordPoll(c0000Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(Preference = 1, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 1))) Bit = 1\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [2, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	c0010Bag := ids.Bag{}
	c0010Bag.Add(c0010)
	tree.RecordPoll(c0010Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 1, SF(Confidence = 1, Finalized = false, SL(Preference = 1))) Bit = 2\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}

	tree.RecordPoll(c0010Bag)
	{
		expected := "SB(NumSuccessfulPolls = 2, SF(Confidence = 2, Finalized = true)) Bits = [3, 256)"
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); c0010 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0010, pref)
		} else if !tree.Finalized() {
			t.Fatalf("Finalized too late")
		}
	}
}

func TestSnowballDoubleAdd(t *testing.T) {
	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 3, BetaRogue: 5,
	}
	tree := Tree{}
	tree.Initialize(params, Red)
	tree.Add(Red)

	{
		expected := initialUnaryDescription
		if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		} else if pref := tree.Preference(); Red != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", Red, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		}
	}
}

func TestSnowballConsistent(t *testing.T) {
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

	if !n.Agreement() {
		t.Fatalf("Network agreed on inconsistent values")
	}
}

func TestSnowballFilterBinaryChildren(t *testing.T) {
	c0000 := ids.ID{0b00000000}
	c1000 := ids.ID{0b00000001}
	c0100 := ids.ID{0b00000010}
	c0010 := ids.ID{0b00000100}

	params := Parameters{
		K: 1, Alpha: 1, BetaVirtuous: 1, BetaRogue: 2,
	}
	tree := Tree{}
	tree.Initialize(params, c0000)
	{
		expected := initialUnaryDescription
		if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		} else if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		}
	}

	tree.Add(c1000)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		} else if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		}
	}

	tree.Add(c0010)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 2)\n" +
			"        SB(Preference = 0, NumSuccessfulPolls[0] = 0, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"            SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"            SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		} else if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		}
	}

	c0000Bag := ids.Bag{}
	c0000Bag.Add(c0000)
	tree.RecordPoll(c0000Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		} else if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		}
	}

	tree.Add(c0100)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 0\n" +
			"    SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"        SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"        SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [1, 256)"
		if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		} else if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		}
	}

	c0100Bag := ids.Bag{}
	c0100Bag.Add(c0100)
	tree.RecordPoll(c0100Bag)
	{
		expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 1, NumSuccessfulPolls[1] = 0, SF(Confidence = 0, Finalized = false, SL(Preference = 0))) Bit = 2\n" +
			"    SB(NumSuccessfulPolls = 1, SF(Confidence = 1, Finalized = true)) Bits = [3, 256)\n" +
			"    SB(NumSuccessfulPolls = 0, SF(Confidence = 0, Finalized = false)) Bits = [3, 256)"
		if pref := tree.Preference(); c0000 != pref {
			t.Fatalf("Wrong preference. Expected %s got %s", c0000, pref)
		} else if tree.Finalized() {
			t.Fatalf("Finalized too early")
		} else if str := tree.String(); expected != str {
			t.Fatalf("Wrong string. Expected:\n%s\ngot:\n%s", expected, str)
		}
	}
}
