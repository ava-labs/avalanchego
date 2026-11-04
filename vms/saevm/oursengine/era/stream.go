package era

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// Write kinds of the stream.
const (
	WAcctGone = 0
	WAcct     = 1
	WSlot     = 2
	WWipe     = 3
	WCode     = 4
)

// W is one state write. Slices alias the decoded frame.
type W struct {
	Kind      byte
	Addr      []byte // 20
	MultiCoin bool
	Nonce     uint64
	Balance   []byte // 32, big-endian
	CodeHash  []byte // 32 (WAcct, WCode)
	Slot      []byte // 32, raw (not masked)
	Value     []byte // 32
	Code      []byte // WCode
}

// Block is one stream block (tradenode era/src/lib.rs BlockRec). Slices alias the decoded
// frame and are reused after the ReadStream callback returns.
type Block struct {
	Number, Timestamp, TimeMs uint64
	BaseFee                   uint64 // the header's (worst-case bound under SAE)
	ExecBaseFee               uint64 // the base fee the block executed with (SAE gas clock)
	GasLimit, GasUsed         uint64
	Hash, Parent, StateRoot   [32]byte
	Coinbase                  [20]byte
	Extra, ExtData            []byte
	BlockJSON                 []byte // eth_getBlockByNumber(n, true) as the RPC sent it
	ReceiptsJSON              []byte // eth_getBlockReceipts(n)
	Groups                    [][]W  // one per tx in order, then one for the block's atomic txs
}

type cur struct{ b []byte }

func (c *cur) take(n int) ([]byte, error) {
	if n < 0 || len(c.b) < n {
		return nil, errTrunc
	}
	x := c.b[:n:n]
	c.b = c.b[n:]
	return x, nil
}
func (c *cur) u32() (uint32, error) {
	x, err := c.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(x), nil
}
func (c *cur) bytes() ([]byte, error) {
	n, err := c.u32()
	if err != nil {
		return nil, err
	}
	return c.take(int(n))
}

func decodeBlock(b []byte, blk *Block) error {
	c := &cur{b}
	h, err := c.take(7*8 + 32*3 + 20)
	if err != nil {
		return err
	}
	u := func(i int) uint64 { return binary.LittleEndian.Uint64(h[8*i:]) }
	blk.Number, blk.Timestamp, blk.TimeMs, blk.BaseFee, blk.ExecBaseFee, blk.GasLimit, blk.GasUsed = u(0), u(1), u(2), u(3), u(4), u(5), u(6)
	copy(blk.Hash[:], h[56:])
	copy(blk.Parent[:], h[88:])
	copy(blk.StateRoot[:], h[120:])
	copy(blk.Coinbase[:], h[152:])
	for _, p := range []*[]byte{&blk.Extra, &blk.ExtData, &blk.BlockJSON, &blk.ReceiptsJSON} {
		if *p, err = c.bytes(); err != nil {
			return err
		}
	}
	ng, err := c.u32()
	if err != nil {
		return err
	}
	if int(ng) > len(c.b) {
		return errTrunc
	}
	blk.Groups = blk.Groups[:0]
	for range ng {
		n, err := c.u32()
		if err != nil {
			return err
		}
		if int(n) > len(c.b) {
			return errTrunc
		}
		g := make([]W, 0, n)
		for range n {
			t, err := c.take(1)
			if err != nil {
				return err
			}
			w := W{Kind: t[0]}
			switch w.Kind {
			case WAcctGone, WWipe:
				w.Addr, err = c.take(20)
			case WAcct:
				var x []byte
				if x, err = c.take(20 + 1 + 8 + 32 + 32); err == nil {
					w.Addr, w.MultiCoin = x[:20], x[20] != 0
					w.Nonce = binary.LittleEndian.Uint64(x[21:])
					w.Balance, w.CodeHash = x[29:61], x[61:93]
				}
			case WSlot:
				var x []byte
				if x, err = c.take(20 + 32 + 32); err == nil {
					w.Addr, w.Slot, w.Value = x[:20], x[20:52], x[52:84]
				}
			case WCode:
				if w.CodeHash, err = c.take(32); err == nil {
					w.Code, err = c.bytes()
				}
			default:
				return fmt.Errorf("era: unknown write tag %d", w.Kind)
			}
			if err != nil {
				return err
			}
			g = append(g, w)
		}
		blk.Groups = append(blk.Groups, g)
	}
	if len(c.b) != 0 {
		return errors.New("era: trailing bytes after a block record")
	}
	return nil
}

// ReadStream calls f for every block of a stream content in order. The Block (and its
// slices) is reused after f returns.
func ReadStream(b []byte, f func(*Block) error) (first, last uint64, err error) {
	if len(b) < 28 || string(b[:8]) != StreamMagic {
		return 0, 0, errors.New("era: not an era stream")
	}
	first, last = binary.LittleEndian.Uint64(b[8:]), binary.LittleEndian.Uint64(b[16:])
	np := int(binary.LittleEndian.Uint32(b[24:]))
	off := 28 + 40*np
	dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return 0, 0, err
	}
	defer dec.Close()
	next := first
	var raw []byte
	blk := new(Block)
	for {
		if off+8 > len(b) {
			return 0, 0, errTrunc
		}
		clen := int(binary.LittleEndian.Uint32(b[off:]))
		n := int(binary.LittleEndian.Uint32(b[off+4:]))
		if clen == 0 {
			break
		}
		if off+8+clen > len(b) {
			return 0, 0, errTrunc
		}
		if raw, err = dec.DecodeAll(b[off+8:off+8+clen], raw[:0]); err != nil {
			return 0, 0, err
		}
		c := &cur{raw}
		for range n {
			rec, err := c.bytes()
			if err != nil {
				return 0, 0, err
			}
			if err := decodeBlock(rec, blk); err != nil {
				return 0, 0, err
			}
			if blk.Number != next {
				return 0, 0, fmt.Errorf("era: block %d where %d was due", blk.Number, next)
			}
			next++
			if err := f(blk); err != nil {
				return 0, 0, err
			}
		}
		off += 8 + clen
	}
	if next != last+1 || off+8 != len(b) {
		return 0, 0, fmt.Errorf("era: stream ends at %d (want %d)", next-1, last)
	}
	return first, last, nil
}
