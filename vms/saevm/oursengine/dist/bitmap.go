package dist

import (
	"errors"
	"fmt"
	"math/bits"

	"github.com/klauspost/compress/zstd"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// Version is the protocol version byte that starts every announce.
const Version = 1

// maxAnnounce bounds a decoded bitmap: 64 MiB of bits = 512M chunks = 512 TB
// of files, far past any node.
const maxAnnounce = 64 << 20

// Bitmap is a set of chunk indices [0, Len). Byte j bit k (least significant
// first) of the wire form is chunk 8j+k.
type Bitmap struct {
	n int64
	w []uint64
}

func NewBitmap(n int64) *Bitmap { return &Bitmap{n: n, w: make([]uint64, (n+63)/64)} }

func (b *Bitmap) Len() int64       { return b.n }
func (b *Bitmap) Has(i int64) bool { return i >= 0 && i < b.n && b.w[i/64]&(1<<(i%64)) != 0 }
func (b *Bitmap) Set(i int64)      { b.w[i/64] |= 1 << (i % 64) }
func (b *Bitmap) Clear(i int64)    { b.w[i/64] &^= 1 << (i % 64) }
func (b *Bitmap) Clone() *Bitmap   { return &Bitmap{n: b.n, w: append([]uint64(nil), b.w...)} }
func (b *Bitmap) HasRange(first, n int64) bool {
	for i := first; i < first+n; i++ {
		if !b.Has(i) {
			return false
		}
	}
	return true
}

func (b *Bitmap) Count() int64 {
	c := 0
	for _, w := range b.w {
		c += bits.OnesCount64(w)
	}
	return int64(c)
}

// Presence is the bitmap of f's present chunks (header chunks included).
func Presence(f *chunkfile.File) *Bitmap {
	b := NewBitmap(f.NumChunks())
	for i := range b.n {
		if f.Present(i) {
			b.Set(i)
		}
	}
	return b
}

var (
	encoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression), zstd.WithSingleSegment(true))
	decoder, _ = zstd.NewReader(nil, zstd.WithDecoderMaxMemory(maxAnnounce), zstd.WithDecoderConcurrency(0))
)

// Announce is the wire form: the Version byte, then one zstd frame holding
// ceil(Len/8) bitmap bytes (bits past Len are zero). Its frame header
// records the content size (single segment frames always carry it).
func (b *Bitmap) Announce() []byte {
	raw := make([]byte, (b.n+7)/8)
	for i := range raw {
		raw[i] = byte(b.w[i/8] >> (8 * (i % 8)))
	}
	return encoder.EncodeAll(raw, []byte{Version})
}

var ErrAnnounce = errors.New("dist: bad announce")

// ParseAnnounce decodes an announce for a file of n chunks.
func ParseAnnounce(msg []byte, n int64) (*Bitmap, error) {
	if len(msg) == 0 || msg[0] != Version {
		return nil, fmt.Errorf("%w: version", ErrAnnounce)
	}
	want := (n + 7) / 8
	if want > maxAnnounce {
		return nil, fmt.Errorf("%w: %d chunks", ErrAnnounce, n)
	}
	var h zstd.Header
	if err := h.Decode(msg[1:]); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAnnounce, err)
	}
	if !h.HasFCS || h.FrameContentSize != uint64(want) {
		return nil, fmt.Errorf("%w: frame holds %d bytes, want %d", ErrAnnounce, h.FrameContentSize, want)
	}
	raw, err := decoder.DecodeAll(msg[1:], make([]byte, 0, want))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAnnounce, err)
	}
	if int64(len(raw)) != want {
		return nil, fmt.Errorf("%w: %d bytes, want %d", ErrAnnounce, len(raw), want)
	}
	b := NewBitmap(n)
	for i, c := range raw {
		b.w[i/8] |= uint64(c) << (8 * (i % 8))
	}
	if n%8 != 0 && raw[want-1]>>(n%8) != 0 {
		return nil, fmt.Errorf("%w: bits past the end", ErrAnnounce)
	}
	return b, nil
}
