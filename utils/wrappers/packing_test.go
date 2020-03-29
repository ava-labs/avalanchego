// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wrappers

import (
	"bytes"
	"testing"
)

const (
	ByteSentinal  = 0
	ShortSentinal = 0
	IntSentinal   = 0
	LongSentinal  = 0
)

func TestPackerPackByte(t *testing.T) {
	p := Packer{MaxSize: 1}

	p.PackByte(0x01)

	if p.Errored() {
		t.Fatal(p.Err)
	}

	if size := len(p.Bytes); size != 1 {
		t.Fatalf("Packer.PackByte wrote %d byte(s) but expected %d byte(s)", size, 1)
	}

	expected := []byte{0x01}
	if !bytes.Equal(p.Bytes, expected) {
		t.Fatalf("Packer.PackByte wrote:\n%v\nExpected:\n%v", p.Bytes, expected)
	}

	p.PackByte(0x02)
	if !p.Errored() {
		t.Fatal("Packer.PackByte did not fail when attempt was beyond p.MaxSize")
	}
}

func TestPackerUnpackByte(t *testing.T) {
	var (
		p                = Packer{Bytes: []byte{0x01}, Offset: 0}
		actual           = p.UnpackByte()
		expected    byte = 1
		expectedLen      = ByteLen
	)
	if p.Errored() {
		t.Fatalf("Packer.UnpackByte unexpectedly raised %s", p.Err)
	} else if actual != expected {
		t.Fatalf("Packer.UnpackByte returned %d, but expected %d", actual, expected)
	} else if p.Offset != expectedLen {
		t.Fatalf("Packer.UnpackByte left Offset %d, expected %d", p.Offset, expectedLen)
	}

	actual = p.UnpackByte()
	if !p.Errored() {
		t.Fatalf("Packer.UnpackByte should have set error, due to attempted out of bounds read")
	} else if actual != ByteSentinal {
		t.Fatalf("Packer.UnpackByte returned %d, expected sentinal value %d", actual, ByteSentinal)
	}
}

func TestPackerPackShort(t *testing.T) {
	p := Packer{MaxSize: 2}

	p.PackShort(0x0102)

	if p.Errored() {
		t.Fatal(p.Err)
	}

	if size := len(p.Bytes); size != 2 {
		t.Fatalf("Packer.PackShort wrote %d byte(s) but expected %d byte(s)", size, 2)
	}

	expected := []byte{0x01, 0x02}
	if !bytes.Equal(p.Bytes, expected) {
		t.Fatalf("Packer.PackShort wrote:\n%v\nExpected:\n%v", p.Bytes, expected)
	}
}

func TestPackerUnpackShort(t *testing.T) {
	var (
		p                  = Packer{Bytes: []byte{0x01, 0x02}, Offset: 0}
		actual             = p.UnpackShort()
		expected    uint16 = 0x0102
		expectedLen        = ShortLen
	)
	if p.Errored() {
		t.Fatalf("Packer.UnpackShort unexpectedly raised %s", p.Err)
	} else if actual != expected {
		t.Fatalf("Packer.UnpackShort returned %d, but expected %d", actual, expected)
	} else if p.Offset != expectedLen {
		t.Fatalf("Packer.UnpackShort left Offset %d, expected %d", p.Offset, expectedLen)
	}

	actual = p.UnpackShort()
	if !p.Errored() {
		t.Fatalf("Packer.UnpackShort should have set error, due to attempted out of bounds read")
	} else if actual != ShortSentinal {
		t.Fatalf("Packer.UnpackShort returned %d, expected sentinal value %d", actual, ShortSentinal)
	}
}

func TestPackerPackInt(t *testing.T) {
	p := Packer{MaxSize: 4}

	p.PackInt(0x01020304)

	if p.Errored() {
		t.Fatal(p.Err)
	}

	if size := len(p.Bytes); size != 4 {
		t.Fatalf("Packer.PackInt wrote %d byte(s) but expected %d byte(s)", size, 4)
	}

	expected := []byte{0x01, 0x02, 0x03, 0x04}
	if !bytes.Equal(p.Bytes, expected) {
		t.Fatalf("Packer.PackInt wrote:\n%v\nExpected:\n%v", p.Bytes, expected)
	}

	p.PackInt(0x05060708)
	if !p.Errored() {
		t.Fatal("Packer.PackInt did not fail when attempt was beyond p.MaxSize")
	}
}

func TestPackerUnpackInt(t *testing.T) {
	var (
		p                  = Packer{Bytes: []byte{0x01, 0x02, 0x03, 0x04}, Offset: 0}
		actual             = p.UnpackInt()
		expected    uint32 = 0x01020304
		expectedLen        = IntLen
	)
	if p.Errored() {
		t.Fatalf("Packer.UnpackInt unexpectedly raised %s", p.Err)
	} else if actual != expected {
		t.Fatalf("Packer.UnpackInt returned %d, but expected %d", actual, expected)
	} else if p.Offset != expectedLen {
		t.Fatalf("Packer.UnpackInt left Offset %d, expected %d", p.Offset, expectedLen)
	}

	actual = p.UnpackInt()
	if !p.Errored() {
		t.Fatalf("Packer.UnpackInt should have set error, due to attempted out of bounds read")
	} else if actual != IntSentinal {
		t.Fatalf("Packer.UnpackInt returned %d, expected sentinal value %d", actual, IntSentinal)
	}
}

func TestPackerPackLong(t *testing.T) {
	p := Packer{MaxSize: 8}

	p.PackLong(0x0102030405060708)

	if p.Errored() {
		t.Fatal(p.Err)
	}

	if size := len(p.Bytes); size != 8 {
		t.Fatalf("Packer.PackLong wrote %d byte(s) but expected %d byte(s)", size, 8)
	}

	expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if !bytes.Equal(p.Bytes, expected) {
		t.Fatalf("Packer.PackLong wrote:\n%v\nExpected:\n%v", p.Bytes, expected)
	}

	p.PackLong(0x090a0b0c0d0e0f00)
	if !p.Errored() {
		t.Fatal("Packer.PackLong did not fail when attempt was beyond p.MaxSize")
	}
}

func TestPackerUnpackLong(t *testing.T) {
	var (
		p                  = Packer{Bytes: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, Offset: 0}
		actual             = p.UnpackLong()
		expected    uint64 = 0x0102030405060708
		expectedLen        = LongLen
	)
	if p.Errored() {
		t.Fatalf("Packer.UnpackLong unexpectedly raised %s", p.Err)
	} else if actual != expected {
		t.Fatalf("Packer.UnpackLong returned %d, but expected %d", actual, expected)
	} else if p.Offset != expectedLen {
		t.Fatalf("Packer.UnpackLong left Offset %d, expected %d", p.Offset, expectedLen)
	}

	actual = p.UnpackLong()
	if !p.Errored() {
		t.Fatalf("Packer.UnpackLong should have set error, due to attempted out of bounds read")
	} else if actual != LongSentinal {
		t.Fatalf("Packer.UnpackLong returned %d, expected sentinal value %d", actual, LongSentinal)
	}
}

func TestPackerPackFixedBytes(t *testing.T) {
	p := Packer{MaxSize: 3}

	p.PackFixedBytes([]byte("Ava"))

	if p.Errored() {
		t.Fatal(p.Err)
	}

	if size := len(p.Bytes); size != 3 {
		t.Fatalf("Packer.PackFixedBytes wrote %d byte(s) but expected %d byte(s)", size, 3)
	}

	expected := []byte("Ava")
	if !bytes.Equal(p.Bytes, expected) {
		t.Fatalf("Packer.PackFixedBytes wrote:\n%v\nExpected:\n%v", p.Bytes, expected)
	}

	p.PackFixedBytes([]byte("Ava"))
	if !p.Errored() {
		t.Fatal("Packer.PackFixedBytes did not fail when attempt was beyond p.MaxSize")
	}
}

func TestPackerUnpackFixedBytes(t *testing.T) {
	var (
		p           = Packer{Bytes: []byte("Ava")}
		actual      = p.UnpackFixedBytes(3)
		expected    = []byte("Ava")
		expectedLen = 3
	)
	if p.Errored() {
		t.Fatalf("Packer.UnpackFixedBytes unexpectedly raised %s", p.Err)
	} else if !bytes.Equal(actual, expected) {
		t.Fatalf("Packer.UnpackFixedBytes returned %d, but expected %d", actual, expected)
	} else if p.Offset != expectedLen {
		t.Fatalf("Packer.UnpackFixedBytes left Offset %d, expected %d", p.Offset, expectedLen)
	}

	actual = p.UnpackFixedBytes(3)
	if !p.Errored() {
		t.Fatalf("Packer.UnpackFixedBytes should have set error, due to attempted out of bounds read")
	} else if actual != nil {
		t.Fatalf("Packer.UnpackFixedBytes returned %v, expected sentinal value %v", actual, nil)
	}
}

func TestPackerPackBytes(t *testing.T) {
	p := Packer{MaxSize: 7}

	p.PackBytes([]byte("Ava"))

	if p.Errored() {
		t.Fatal(p.Err)
	}

	if size := len(p.Bytes); size != 7 {
		t.Fatalf("Packer.PackBytes wrote %d byte(s) but expected %d byte(s)", size, 7)
	}

	expected := []byte("\x00\x00\x00\x03Ava")
	if !bytes.Equal(p.Bytes, expected) {
		t.Fatalf("Packer.PackBytes wrote:\n%v\nExpected:\n%v", p.Bytes, expected)
	}

	p.PackBytes([]byte("Ava"))
	if !p.Errored() {
		t.Fatal("Packer.PackBytes did not fail when attempt was beyond p.MaxSize")
	}
}

func TestPackerUnpackBytes(t *testing.T) {
	var (
		p           = Packer{Bytes: []byte("\x00\x00\x00\x03Ava")}
		actual      = p.UnpackBytes()
		expected    = []byte("Ava")
		expectedLen = 7
	)
	if p.Errored() {
		t.Fatalf("Packer.UnpackBytes unexpectedly raised %s", p.Err)
	} else if !bytes.Equal(actual, expected) {
		t.Fatalf("Packer.UnpackBytes returned %d, but expected %d", actual, expected)
	} else if p.Offset != expectedLen {
		t.Fatalf("Packer.UnpackBytes left Offset %d, expected %d", p.Offset, expectedLen)
	}

	actual = p.UnpackBytes()
	if !p.Errored() {
		t.Fatalf("Packer.UnpackBytes should have set error, due to attempted out of bounds read")
	} else if actual != nil {
		t.Fatalf("Packer.UnpackBytes returned %v, expected sentinal value %v", actual, nil)
	}
}

func TestPackerString(t *testing.T) {
	p := Packer{MaxSize: 5}

	p.PackStr("Ava")

	if p.Errored() {
		t.Fatal(p.Err)
	}

	if size := len(p.Bytes); size != 5 {
		t.Fatalf("Packer.PackStr wrote %d byte(s) but expected %d byte(s)", size, 5)
	}

	expected := []byte{0x00, 0x03, 0x41, 0x76, 0x61}
	if !bytes.Equal(p.Bytes, expected) {
		t.Fatalf("Packer.PackStr wrote:\n%v\nExpected:\n%v", p.Bytes, expected)
	}
}

func TestPacker(t *testing.T) {
	packer := Packer{
		MaxSize: 3,
	}

	if packer.Errored() {
		t.Fatalf("Packer has error %s", packer.Err)
	}

	packer.PackShort(17)
	if len(packer.Bytes) != 2 {
		t.Fatalf("Wrong byte length")
	}

	packer.PackShort(1)
	if !packer.Errored() {
		t.Fatalf("Packer should have error")
	}

	newPacker := Packer{
		Bytes: packer.Bytes,
	}

	if newPacker.UnpackShort() != 17 {
		t.Fatalf("Unpacked wrong value")
	}
}

func TestPackBool(t *testing.T) {
	p := Packer{MaxSize: 3}
	p.PackBool(false)
	p.PackBool(true)
	p.PackBool(false)
	if p.Errored() {
		t.Fatal("should have been able to pack 3 bools")
	}

	p2 := Packer{Bytes: p.Bytes}
	bool1, bool2, bool3 := p2.UnpackBool(), p2.UnpackBool(), p2.UnpackBool()

	if p.Errored() {
		t.Fatalf("errors while unpacking bools: %v", p.Errs)
	}

	if bool1 || !bool2 || bool3 {
		t.Fatal("got back wrong values")
	}
}
