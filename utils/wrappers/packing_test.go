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

func TestPackerInt(t *testing.T) {
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
}

func TestPackerLong(t *testing.T) {
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
