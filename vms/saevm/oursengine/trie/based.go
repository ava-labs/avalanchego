package trie

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/era"
)

// WriteBased writes t as the top file of nb, the base that t's base with t's overlay folds into
// (a fold's new base: nb.Block() == t.Block, same state): every trie keeps t's depth and
// positions, so the file holds the node's applied top, and only the account offsets and the cut
// bounds are new, taken from one pass over nb's state records (no hashing). The account offsets
// and the storage trie records go through work files in dir during the pass and nb's pages are
// dropped behind it, so the heap holds the account trie's bounds and one storage trie's bounds.
// The owners are the accounts with storage in nb; a trie of t with keys but no storage in nb,
// or storage in nb without a trie in t, is an error (nb is not t's state). t must have no
// changes pending (after Root). Returns the file's path, hash and size.
func (t *Top) WriteBased(dir string, nb *base.Base) (string, chunkfile.Hash, int64, error) {
	e := t.E
	if len(e.dirtyStor) > 0 || len(e.dirtyAcct) > 0 {
		return "", chunkfile.Hash{}, 0, errors.New("trie: WriteBased with changes pending (call Root first)")
	}
	return writeBased(dir, nb, &trieSrc{K: e.K, Five: e.FiveFields, Block: t.Block, Root: t.Root, acct: e.acct, trieOf: e.trie, tries: e.storageTries()})
}

// trieSrc is a top's layout: its cut depths and positions, the bounds aside.
type trieSrc struct {
	K      int
	Five   bool
	Block  uint64
	Root   [32]byte
	acct   *Trie
	trieOf func(ah *[32]byte) *Trie // nil: none (no keys)
	tries  uint64                   // storage tries with keys
}

func writeBased(dir string, nb *base.Base, src *trieSrc) (string, chunkfile.Hash, int64, error) {
	if nb.Block() != src.Block {
		return "", chunkfile.Hash{}, 0, fmt.Errorf("trie: top at block %d, base at block %d", src.Block, nb.Block())
	}
	acctPath, storPath := filepath.Join(dir, "top.acct"), filepath.Join(dir, "top.stor")
	defer os.Remove(acctPath)
	defer os.Remove(storPath)
	af, err := os.Create(acctPath)
	if err != nil {
		return "", chunkfile.Hash{}, 0, err
	}
	defer af.Close()
	sf, err := os.Create(storPath)
	if err != nil {
		return "", chunkfile.Hash{}, 0, err
	}
	defer sf.Close()
	chunkfile.NoPrealloc(af)
	chunkfile.NoPrealloc(sf)
	aw, sw := bufio.NewWriterSize(af, 1<<20), bufio.NewWriterSize(sf, 1<<20)
	abounds, nacct, ntries, err := basedPass(src, nb, aw, sw)
	if err != nil {
		return "", chunkfile.Hash{}, 0, err
	}
	if err := errors.Join(aw.Flush(), sw.Flush()); err != nil {
		return "", chunkfile.Hash{}, 0, err
	}
	if want := src.tries; want != ntries {
		return "", chunkfile.Hash{}, 0, fmt.Errorf("trie: %d storage tries with keys in the top, %d with storage in the base", want, ntries)
	}

	tmp := filepath.Join(dir, "top.tmp")
	cw, err := chunkfile.Create(tmp)
	if err != nil {
		return "", chunkfile.Hash{}, 0, err
	}
	bw := bufio.NewWriterSize(cw, 4<<20)
	s := nb.Snap()
	h := make([]byte, topHeader)
	copy(h, topMagic)
	binary.LittleEndian.PutUint32(h[8:], uint32(src.K))
	binary.LittleEndian.PutUint32(h[12:], uint32(src.acct.depth))
	binary.LittleEndian.PutUint64(h[16:], s.Block)
	binary.LittleEndian.PutUint64(h[24:], uint64(len(s.B)))
	binary.LittleEndian.PutUint64(h[32:], nacct)
	binary.LittleEndian.PutUint64(h[40:], ntries)
	copy(h[48:], src.Root[:])
	if src.Five {
		h[80] = 1
	}
	bw.Write(h)
	err = copyFrom(bw, af)
	if err == nil {
		writeTrie(bw, src.acct, abounds)
		err = copyFrom(bw, sf)
	}
	if err == nil {
		err = bw.Flush()
	}
	if err != nil {
		cw.Finish()
		os.Remove(tmp)
		return "", chunkfile.Hash{}, 0, err
	}
	os.Remove(acctPath) // copied: free them before the top file's own copy
	os.Remove(storPath)
	hash, err := cw.Finish()
	if err != nil {
		return "", chunkfile.Hash{}, 0, err
	}
	path := filepath.Join(dir, hex.EncodeToString(hash[:])+".top")
	if err := os.Rename(tmp, path); err != nil {
		return "", hash, 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", hash, 0, err
	}
	return path, hash, st.Size(), nil
}

// basedPass walks nb's state records once: it writes the account offsets (u64 each) to aw and
// each storage trie record (owner, depth, bounds in nb, t's positions) to sw, ascending by owner,
// and returns the account trie's bounds (ordinals), the account count and the record count.
func basedPass(src *trieSrc, nb *base.Base, aw, sw io.Writer) (abounds []uint64, nacct, ntries uint64, err error) {
	s := nb.Snap()
	at := src.acct
	abounds = make([]uint64, at.cuts()+1)
	nextA := 0
	var (
		r       era.Rec
		b8      [8]byte
		owner   [32]byte
		cur     *Trie    // the owner's trie once a slot of it was seen
		sb      []uint64 // its bounds in nb
		nextS   int      // its next cut whose start is unset
		slotEnd uint64   // the end of its last slot
		dropped uint64   // nb's pages below this were dropped
	)
	finish := func() {
		if cur == nil {
			return
		}
		for ; nextS < len(sb); nextS++ {
			sb[nextS] = slotEnd
		}
		sw.Write(owner[:])
		sw.Write([]byte{cur.depth})
		writeTrie(sw, cur, sb)
		ntries++
		cur = nil
	}
	for f := range s.NState {
		_, off, n := s.Frame(f)
		if err := nb.Check(off, uint64(n)); err != nil {
			return nil, 0, 0, BaseError{err}
		}
		b, p := s.B[off:off+uint64(n)], off
		for len(b) > 0 {
			if err := era.Decode(b, &r); err != nil {
				return nil, 0, 0, err
			}
			switch r.Tag {
			case era.TagAccount:
				finish()
				owner = [32]byte(r.Key)
				for c := int(at.cutOf(&owner)); nextA <= c; nextA++ {
					abounds[nextA] = nacct
				}
				binary.LittleEndian.PutUint64(b8[:], p)
				aw.Write(b8[:])
				nacct++
			case era.TagContext:
				if [32]byte(r.Key) != owner {
					return nil, 0, 0, fmt.Errorf("trie: base context record of %x inside the account %x", r.Key, owner)
				}
			case era.TagSlot:
				if cur == nil {
					// with storageTries' count this makes the owners exactly t's non-empty tries
					if cur = src.trieOf(&owner); cur == nil || cur.at(0).Kind == kEmpty {
						return nil, 0, 0, fmt.Errorf("trie: the base has storage of %x, the top no keys", owner)
					}
					sb, nextS = make([]uint64, cur.cuts()+1), 0
				}
				for c := int(cur.cutOf((*[32]byte)(r.Key))); nextS <= c; nextS++ {
					sb[nextS] = p
				}
				slotEnd = p + uint64(r.Len)
			default:
				return nil, 0, 0, fmt.Errorf("trie: record tag %d in a base's state", r.Tag)
			}
			b = b[r.Len:]
			p += uint64(r.Len)
		}
		if p-dropped >= 64<<20 {
			dropPages(s.B[dropped:p])
			dropped = p
		}
	}
	finish()
	for ; nextA < len(abounds); nextA++ {
		abounds[nextA] = nacct
	}
	return abounds, nacct, ntries, nil
}

// storageTries counts t's storage tries that hold keys (a non-empty root).
func (e *Engine) storageTries() uint64 {
	var n uint64
	for _, tr := range e.stor {
		if tr != nil && tr.at(0).Kind != kEmpty {
			n++
		}
	}
	if e.top != nil {
		e.top.each(func(ah [32]byte, off uint64) {
			if _, touched := e.stor[ah]; !touched && e.top.root(off).Kind != kEmpty {
				n++
			}
		})
	}
	return n
}

// writeTrie writes bounds then tr's positions (levels 0..depth) in the top file's form.
func writeTrie(w io.Writer, tr *Trie, bounds []uint64) {
	var b [posSize]byte
	for _, x := range bounds {
		binary.LittleEndian.PutUint64(b[:8], x)
		w.Write(b[:8])
	}
	for i := range levelOff(int(tr.depth) + 1) {
		p := tr.at(i)
		b = [posSize]byte{}
		copy(b[:], p.Ref[:p.N])
		b[32], b[33] = p.N, p.Kind
		w.Write(b[:])
	}
}

func copyFrom(w io.Writer, f *os.File) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := io.Copy(w, f)
	return err
}

// dropPages drops the whole pages of the mapped range b from the process (advice only: a
// failure costs RSS, not data; the pages stay in the page cache).
func dropPages(b []byte) {
	if len(b) == 0 {
		return
	}
	page := uintptr(os.Getpagesize())
	start := uintptr(unsafe.Pointer(&b[0]))
	lo := int((page - start%page) % page)
	hi := len(b) - int((start+uintptr(len(b)))%page)
	if hi > lo {
		unix.Madvise(b[lo:hi], unix.MADV_DONTNEED)
	}
}
