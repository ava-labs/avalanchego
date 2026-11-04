package cblock

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Container kinds: how a C-Chain consensus container wraps the coreth block
// (avalanchego vms/proposervm/block at 4f8c6316bed8, linearcodec version 0).
type Kind uint8

const (
	// PreFork is a bare coreth block, from before the proposervm fork (Apricot Phase 4).
	PreFork Kind = iota
	// Signed is a statelessBlock (type id 0) with a proposer certificate and signature.
	Signed
	// Unsigned is a statelessBlock with no certificate and no signature (any validator may
	// build it once the proposer windows have passed).
	Unsigned
	// GraniteSigned is a statelessGraniteBlock (type id 2) with certificate and signature.
	GraniteSigned
	// GraniteUnsigned is a statelessGraniteBlock with neither.
	GraniteUnsigned
	// Option is an option block (type id 1) of an oracle block; the C-Chain VM is not an
	// oracle VM, but the codec allows it.
	Option
)

func (k Kind) String() string {
	return [...]string{"prefork", "signed", "unsigned", "granite-signed", "granite-unsigned", "option"}[k]
}

// Epoch is the Granite proposervm epoch.
type Epoch struct {
	PChainHeight uint64
	Number       uint64
	StartTime    int64
}

// Container is a parsed consensus container. Byte slices alias the input.
type Container struct {
	Kind         Kind
	ParentID     [32]byte // proposervm parent container ID (zero for PreFork)
	Timestamp    int64    // unix seconds (Signed, Unsigned, Granite*)
	PChainHeight uint64
	Certificate  []byte // DER X.509 staking certificate (signed kinds)
	Epoch        Epoch  // Granite*
	Signature    []byte // signed kinds

	Bytes []byte // the whole container
	// Pre | Inner | Post is the container byte for byte. For the proposervm kinds Pre ends
	// with Inner's u32 length; for PreFork Pre and Post are empty.
	Pre, Inner, Post []byte
	// idEnd: the container ID is sha256 of the first idEnd bytes (proposervm kinds).
	idEnd int
}

var (
	errCodecVersion        = errors.New("cblock: proposervm codec version is not 0")
	errCodecTypeID         = errors.New("cblock: unknown proposervm block type id")
	errCodecShort          = errors.New("cblock: proposervm container truncated")
	errCodecExtra          = errors.New("cblock: proposervm container has trailing bytes")
	errUnexpectedSignature = errors.New("cblock: proposervm signature without certificate")
	errZeroEpoch           = errors.New("cblock: granite proposervm block with zero epoch")
)

type packer struct {
	b   []byte
	off int
	err error
}

func (p *packer) take(n int) []byte {
	if p.err != nil {
		return nil
	}
	if n < 0 || n > len(p.b)-p.off {
		p.err = errCodecShort
		return nil
	}
	x := p.b[p.off : p.off+n : p.off+n]
	p.off += n
	return x
}

func (p *packer) u16() uint16 {
	if x := p.take(2); x != nil {
		return binary.BigEndian.Uint16(x)
	}
	return 0
}

func (p *packer) u32() uint32 {
	if x := p.take(4); x != nil {
		return binary.BigEndian.Uint32(x)
	}
	return 0
}

func (p *packer) u64() uint64 {
	if x := p.take(8); x != nil {
		return binary.BigEndian.Uint64(x)
	}
	return 0
}

// bytes reads a u32 length-prefixed byte slice (reflectcodec: length at most MaxInt32).
func (p *packer) bytes() []byte {
	n := p.u32()
	if p.err == nil && n > math.MaxInt32 {
		p.err = fmt.Errorf("cblock: proposervm slice of %d bytes", n)
	}
	return p.take(int(n))
}

// parsePostFork mirrors block.ParseWithoutVerification plus the structural part of verify:
// no signature without a certificate, non-zero Granite epoch. The certificate and the
// signature itself are not checked (that is consensus' job; the container ID covers them).
func parsePostFork(b []byte) (*Container, error) {
	p := &packer{b: b}
	if p.u16() != 0 {
		if p.err != nil {
			return nil, p.err
		}
		return nil, errCodecVersion
	}
	c := &Container{Bytes: b}
	switch typ := p.u32(); {
	case p.err != nil:
		return nil, p.err
	case typ == 1:
		c.Kind = Option
		copy(c.ParentID[:], p.take(32))
		c.Inner = p.bytes()
		c.Pre = b[:p.off-len(c.Inner)]
		c.Post = b[p.off:p.off]
		c.idEnd = len(b)
	case typ == 0 || typ == 2:
		copy(c.ParentID[:], p.take(32))
		c.Timestamp = int64(p.u64())
		c.PChainHeight = p.u64()
		c.Certificate = p.bytes()
		c.Inner = p.bytes()
		c.Pre = b[:p.off-len(c.Inner)]
		innerEnd := p.off
		if typ == 2 {
			c.Epoch = Epoch{p.u64(), p.u64(), int64(p.u64())}
		}
		c.idEnd = p.off
		c.Signature = p.bytes()
		c.Post = b[innerEnd:p.off]
		signed := len(c.Certificate) > 0
		switch {
		case p.err != nil:
		case !signed && len(c.Signature) > 0:
			return nil, errUnexpectedSignature
		case typ == 2 && c.Epoch == (Epoch{}):
			return nil, errZeroEpoch
		}
		c.Kind = Unsigned
		if signed {
			c.Kind = Signed
		}
		if typ == 2 {
			c.Kind += GraniteSigned - Signed
		}
	default:
		return nil, fmt.Errorf("%w: %d", errCodecTypeID, typ)
	}
	if p.err != nil {
		return nil, p.err
	}
	if p.off != len(b) {
		return nil, errCodecExtra
	}
	return c, nil
}

// ParseContainer splits a consensus container. Like proposervm's ParseBlock it first tries
// the proposervm codec and otherwise takes the bytes as a pre-fork coreth block, which must
// then be exactly one RLP list (its content is checked by Decode). A coreth block starts
// with an RLP list byte (>= 0xC0), never with the codec version 0x0000, so the two never
// overlap.
func ParseContainer(b []byte) (*Container, error) {
	c, err := parsePostFork(b)
	if err == nil {
		return c, nil
	}
	list, _, rest, rerr := split(b)
	if rerr != nil || !list || len(rest) != 0 {
		return nil, fmt.Errorf("cblock: neither a proposervm container (%v) nor a pre-fork block", err)
	}
	return &Container{Kind: PreFork, Bytes: b, Pre: b[:0], Inner: b, Post: b[len(b):]}, nil
}

// SplitContainer returns pre, inner, post with pre|inner|post == b.
func SplitContainer(b []byte) (pre, inner, post []byte, err error) {
	c, err := ParseContainer(b)
	if err != nil {
		return nil, nil, nil, err
	}
	return c.Pre, c.Inner, c.Post, nil
}

// ID is the consensus container ID. Signed and unsigned blocks: sha256 of the bytes without
// the trailing signature and its u32 length (proposervm statelessBlockMetadata.initialize).
// Option: sha256 of all bytes. PreFork: the coreth block hash, keccak256 of the header RLP.
func (c *Container) ID() ([32]byte, error) {
	if c.Kind != PreFork {
		return sha256.Sum256(c.Bytes[:c.idEnd]), nil
	}
	_, content, _, err := split(c.Inner)
	if err != nil {
		return [32]byte{}, err
	}
	d := &dec{b: content}
	h := d.raw()
	if d.err != nil {
		return [32]byte{}, d.err
	}
	return keccak(h), nil
}
