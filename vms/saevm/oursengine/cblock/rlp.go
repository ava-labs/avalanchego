package cblock

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Strict RLP with the canonical rules of libevm rlp (decode.go readKind, Stream.uint,
// decodeBigInt, decodeByteArray): a single byte below 0x80 must not be wrapped in a string,
// long sizes must be >= 56 and have no leading zero bytes, integers have no leading zero
// bytes (zero is 0x80, a bare 0x00 is rejected), fixed arrays need their exact length, and a
// list must hold exactly the elements its decoder consumes.

var (
	errCanonSize = errors.New("rlp: non-canonical size information")
	errCanonInt  = errors.New("rlp: non-canonical integer format")
	errTooShort  = errors.New("rlp: value size exceeds available input length")
	errExpString = errors.New("rlp: expected string or byte")
	errExpList   = errors.New("rlp: expected list")
	errMoreElems = errors.New("rlp: input list has too many elements")
	errFewElems  = errors.New("rlp: too few elements")
	errTrailing  = errors.New("rlp: input contains more than one value")
)

// split reads one item from b: whether it is a list, its content and the bytes after it.
func split(b []byte) (list bool, content, rest []byte, err error) {
	if len(b) == 0 {
		return false, nil, nil, errFewElems
	}
	p := b[0]
	var hdr, size uint64
	switch {
	case p < 0x80:
		return false, b[:1], b[1:], nil
	case p < 0xB8:
		hdr, size = 1, uint64(p-0x80)
		if size == 1 && len(b) > 1 && b[1] < 0x80 {
			return false, nil, nil, errCanonSize
		}
	case p < 0xC0:
		hdr, size, err = longSize(b, int(p-0xB7))
		if err != nil {
			return false, nil, nil, err
		}
	case p < 0xF8:
		list, hdr, size = true, 1, uint64(p-0xC0)
	default:
		list = true
		hdr, size, err = longSize(b, int(p-0xF7))
		if err != nil {
			return false, nil, nil, err
		}
	}
	if size > uint64(len(b))-hdr {
		return false, nil, nil, errTooShort
	}
	return list, b[hdr : hdr+size], b[hdr+size:], nil
}

func longSize(b []byte, n int) (hdr, size uint64, err error) {
	if len(b) < 1+n {
		return 0, 0, errTooShort
	}
	if b[1] == 0 {
		return 0, 0, errCanonSize
	}
	for _, c := range b[1 : 1+n] {
		size = size<<8 | uint64(c)
	}
	if size < 56 {
		return 0, 0, errCanonSize
	}
	return uint64(1 + n), size, nil
}

// dec reads the elements of one list in order. The first error sticks.
type dec struct {
	b   []byte
	err error
}

func (d *dec) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

func (d *dec) item() (list bool, c []byte) {
	if d.err != nil {
		return false, nil
	}
	list, c, rest, err := split(d.b)
	if err != nil {
		d.fail(err)
		return false, nil
	}
	d.b = rest
	return list, c
}

func (d *dec) more() bool { return d.err == nil && len(d.b) > 0 }

// raw returns the full encoding of the next item.
func (d *dec) raw() []byte {
	start := d.b
	d.item()
	if d.err != nil {
		return nil
	}
	return start[:len(start)-len(d.b)]
}

func (d *dec) bytes() []byte {
	list, c := d.item()
	if list {
		d.fail(errExpString)
	}
	if d.err != nil {
		return nil
	}
	return c
}

func (d *dec) fixed(dst []byte) {
	c := d.bytes()
	if d.err == nil && len(c) != len(dst) {
		d.fail(fmt.Errorf("rlp: input string has %d bytes, want %d", len(c), len(dst)))
	}
	copy(dst, c)
}

// uint returns an integer of at most n bytes.
func (d *dec) uint(n int) uint64 {
	c := d.bytes()
	switch {
	case d.err != nil:
		return 0
	case len(c) > n:
		d.fail(fmt.Errorf("rlp: integer of %d bytes, at most %d allowed", len(c), n))
		return 0
	case len(c) > 0 && c[0] == 0:
		d.fail(errCanonInt)
		return 0
	}
	var v uint64
	for _, x := range c {
		v = v<<8 | uint64(x)
	}
	return v
}

func (d *dec) u64() uint64 { return d.uint(8) }

func (d *dec) u256() (v U256) {
	c := d.bytes()
	switch {
	case d.err != nil:
	case len(c) > 32:
		d.fail(fmt.Errorf("rlp: integer of %d bytes is over 256 bits", len(c)))
	case len(c) > 0 && c[0] == 0:
		d.fail(errCanonInt)
	default:
		copy(v[32-len(c):], c)
	}
	return v
}

func (d *dec) list() *dec {
	list, c := d.item()
	if d.err == nil && !list {
		d.fail(errExpList)
	}
	return &dec{b: c, err: d.err}
}

// absorb takes over the error of a nested list.
func (d *dec) absorb(sub *dec) {
	if sub.err != nil {
		d.fail(sub.err)
	}
}

// end fails if elements are left; it also takes over the error of a nested list.
func (d *dec) end(sub *dec) {
	if sub != nil && sub.err != nil {
		d.fail(sub.err)
	}
	if d.err == nil && len(d.b) > 0 {
		d.fail(errMoreElems)
	}
}

// top decodes b as exactly one list and calls f with it.
func top(b []byte, f func(*dec)) error {
	list, c, rest, err := split(b)
	switch {
	case err != nil:
		return err
	case !list:
		return errExpList
	case len(rest) > 0:
		return errTrailing
	}
	d := &dec{b: c}
	f(d)
	d.end(nil)
	return d.err
}

// Encoding: values are appended to a buffer; a list is closed by wrapList, which moves the
// payload right by the header size.

func appendBytes(dst, b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return append(dst, b[0])
	}
	return append(appendHead(dst, 0x80, uint64(len(b))), b...)
}

func appendHead(dst []byte, base byte, size uint64) []byte {
	if size < 56 {
		return append(dst, base+byte(size))
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], size)
	n := 0
	for n < 7 && buf[n] == 0 {
		n++
	}
	dst = append(dst, base+55+byte(8-n))
	return append(dst, buf[n:]...)
}

func appendUint(dst []byte, v uint64) []byte {
	if v == 0 {
		return append(dst, 0x80)
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	n := 0
	for buf[n] == 0 {
		n++
	}
	return appendBytes(dst, buf[n:])
}

func appendU256(dst []byte, v *U256) []byte {
	n := 0
	for n < 32 && v[n] == 0 {
		n++
	}
	return appendBytes(dst, v[n:])
}

// wrapList turns dst[start:] into a list.
func wrapList(dst []byte, start int) []byte {
	size := uint64(len(dst) - start)
	h := appendHead(nil, 0xC0, size)
	dst = append(dst, h...)
	copy(dst[start+len(h):], dst[start:start+int(size)])
	copy(dst[start:], h)
	return dst
}
