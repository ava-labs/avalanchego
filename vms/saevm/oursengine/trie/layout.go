package trie

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/base"
	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// The layout file keeps a top's cut depths and positions without the base they were laid over:
// what a fold needs to write the new top once the new base exists, after the old base may be
// gone (a fold punches it). It holds the account trie and every storage trie touched since the
// top file the engine was opened from (named by its hash, the rest is read there); an engine
// built from a base (no top file) writes every trie. Integers little-endian:
//
//	"FFLAYT01" | K u32 | five fields u8 | account depth u8 | block u64 | root 32 |
//	top file hash 32 (zero: none) | records u64 | account positions |
//	records ascending by owner: owner 32 | depth u8 (0xff: no trie or no keys) | positions |
//	crc32c of everything before u32
const layoutMagic = "FFLAYT01"

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// WriteLayout writes t's layout to path (fsynced, then renamed into place). from is the hash
// of the top file t was opened from: the layout then holds only the tries touched since; zero
// (or a built top) writes every trie. t must have no changes pending.
func (t *Top) WriteLayout(path string, from chunkfile.Hash) error {
	e := t.E
	if len(e.dirtyStor) > 0 || len(e.dirtyAcct) > 0 {
		return errors.New("trie: WriteLayout with changes pending (call Root first)")
	}
	if e.top == nil {
		from = chunkfile.Hash{}
	}
	owners := make([][32]byte, 0, len(e.stor))
	for ah := range e.stor {
		owners = append(owners, ah)
	}
	if from == (chunkfile.Hash{}) && e.top != nil { // every trie: the untouched ones from the file
		e.top.each(func(ah [32]byte, _ uint64) {
			if _, ok := e.stor[ah]; !ok {
				owners = append(owners, ah)
			}
		})
	}
	slices.SortFunc(owners, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	f, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	defer f.Close()
	crc := crc32.New(castagnoli)
	bw := bufio.NewWriterSize(f, 1<<20)
	w := func(b []byte) { bw.Write(b); crc.Write(b) }
	h := []byte(layoutMagic)
	h = binary.LittleEndian.AppendUint32(h, uint32(e.K))
	h = append(h, boolByte(e.FiveFields), e.acct.depth)
	h = binary.LittleEndian.AppendUint64(h, t.Block)
	h = append(append(h, t.Root[:]...), from[:]...)
	h = binary.LittleEndian.AppendUint64(h, uint64(len(owners)))
	w(h)
	pos := func(tr *Trie) {
		var b [posSize]byte
		for i := range levelOff(int(tr.depth) + 1) {
			p := tr.at(i)
			b = [posSize]byte{}
			copy(b[:], p.Ref[:p.N])
			b[32], b[33] = p.N, p.Kind
			w(b[:])
		}
	}
	pos(e.acct)
	for _, ah := range owners {
		w(ah[:])
		tr, ok := e.stor[ah]
		if !ok {
			tr = e.trie(&ah)
		}
		if tr == nil || tr.at(0).Kind == kEmpty {
			w([]byte{0xff})
			continue
		}
		w([]byte{tr.depth})
		pos(tr)
	}
	bw.Write(binary.LittleEndian.AppendUint32(nil, crc.Sum32()))
	if err := bw.Flush(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func boolByte(x bool) byte {
	if x {
		return 1
	}
	return 0
}

// WriteBasedLayout writes the top file of nb from the layout at layoutPath: the tries the layout
// holds, the others from the top file at fromPath (the one the layout names; unused when it
// names none). nb is the base the layout's state folds into (see WriteBased).
// It also returns the layout's state root.
func WriteBasedLayout(layoutPath, fromPath, dir string, nb *base.Base) (path string, hash chunkfile.Hash, size int64, root [32]byte, err error) {
	lm, err := mapFile(layoutPath)
	if err != nil {
		return "", chunkfile.Hash{}, 0, root, err
	}
	defer syscall.Munmap(lm)
	const hl = 8 + 4 + 2 + 8 + 32 + 32 + 8
	if len(lm) < hl+4 || string(lm[:8]) != layoutMagic ||
		crc32.Checksum(lm[:len(lm)-4], castagnoli) != binary.LittleEndian.Uint32(lm[len(lm)-4:]) {
		return "", chunkfile.Hash{}, 0, root, fmt.Errorf("trie: %s is not a whole layout file", layoutPath)
	}
	src := &trieSrc{K: int(binary.LittleEndian.Uint32(lm[8:])), Five: lm[12] == 1, Block: binary.LittleEndian.Uint64(lm[14:])}
	ad := lm[13]
	copy(src.Root[:], lm[22:54])
	from := chunkfile.Hash(lm[54:86])
	nrec := binary.LittleEndian.Uint64(lm[86:])
	body := lm[hl : len(lm)-4]
	take := func(n uint64) ([]byte, error) {
		if n > uint64(len(body)) {
			return nil, errors.New("trie: layout file cut short")
		}
		b := body[:n]
		body = body[n:]
		return b, nil
	}
	ptrie := func(d uint8) (*Trie, error) {
		if d > 7 {
			return nil, errors.New("trie: layout depth")
		}
		b, err := take(posSize * uint64(levelOff(int(d)+1)))
		if err != nil {
			return nil, err
		}
		return &Trie{depth: d, pos: posAt(b, levelOff(int(d)+1)), ro: true}, nil
	}
	if src.acct, err = ptrie(ad); err != nil {
		return "", chunkfile.Hash{}, 0, root, err
	}
	held := make(map[[32]byte]*Trie, nrec) // nil: no trie
	for range nrec {
		hb, err := take(33)
		if err != nil {
			return "", chunkfile.Hash{}, 0, root, err
		}
		var tr *Trie
		if hb[32] != 0xff {
			if tr, err = ptrie(hb[32]); err != nil {
				return "", chunkfile.Hash{}, 0, root, err
			}
			src.tries++
		}
		held[[32]byte(hb[:32])] = tr
	}
	if len(body) != 0 {
		return "", chunkfile.Hash{}, 0, root, errors.New("trie: layout file has trailing bytes")
	}
	var tf *topFile
	if from != (chunkfile.Hash{}) {
		cf, err := chunkfile.Open(fromPath, from)
		if err != nil {
			return "", chunkfile.Hash{}, 0, root, err
		}
		m, err := mapFile(fromPath)
		cf.Close()
		if err != nil {
			return "", chunkfile.Hash{}, 0, root, err
		}
		defer syscall.Munmap(m)
		if tf, err = topRecords(m[int64(len(m))-cf.DataSize():]); err != nil {
			return "", chunkfile.Hash{}, 0, root, fmt.Errorf("trie: %s: %w", fromPath, err)
		}
		tf.each(func(ah [32]byte, off uint64) {
			if _, ok := held[ah]; !ok && tf.root(off).Kind != kEmpty {
				src.tries++
			}
		})
	}
	src.trieOf = func(ah *[32]byte) *Trie {
		if tr, ok := held[*ah]; ok {
			return tr
		}
		if tf == nil {
			return nil
		}
		off, ok := tf.find(ah)
		if !ok {
			return nil
		}
		tr := tf.trie(off)
		return &tr
	}
	path, hash, size, err = writeBased(dir, nb, src)
	return path, hash, size, src.Root, err
}

func mapFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
}

// topRecords indexes the storage trie records of a top file's data part m without its base
// (the layout's positions come from here; the bounds are rewritten anyway).
func topRecords(m []byte) (*topFile, error) {
	if len(m) < topHeader || string(m[:8]) != topMagic {
		return nil, errTop
	}
	adepth := binary.LittleEndian.Uint32(m[12:])
	nacct, ntries := binary.LittleEndian.Uint64(m[32:]), binary.LittleEndian.Uint64(m[40:])
	if adepth > 7 {
		return nil, errTop
	}
	off := uint64(topHeader) + 8*nacct + 8*(uint64(1)<<(4*adepth)+1) + posSize*uint64(levelOff(int(adepth)+1))
	f := &topFile{m: m, tries: off}
	for f.bits < 40 && uint64(8)<<f.bits < ntries {
		f.bits++
	}
	f.bucket = make([]uint64, 1<<f.bits+1)
	next := uint64(0)
	for range ntries {
		if off+33 > uint64(len(m)) || m[off+32] > 7 {
			return nil, errTop
		}
		ah := [32]byte(m[off : off+32])
		for p := f.prefix(&ah); next <= p; next++ {
			f.bucket[next] = off
		}
		off += recLen(m[off+32])
	}
	if off != uint64(len(m)) {
		return nil, errTop
	}
	f.end = off
	for ; next < uint64(len(f.bucket)); next++ {
		f.bucket[next] = off
	}
	return f, nil
}
