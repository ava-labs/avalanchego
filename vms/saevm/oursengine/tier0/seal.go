package tier0

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// The async cut. At the cut the open tier0 is sealed: its files move into a sealed directory
// sealed.<first block, 20 digits> under the writer's directory, with a seal file that records
// the tables and CRCs, and an empty open tier0 starts. The sealed records are readable at once
// through Sealed.File (raw, the same read API as the final file) while one side goroutine per
// writer writes the final v6 file and its sidecar, in seal order, from the sealed spools: the
// bytes are those of a synchronous cut (both go through writeV6 with the same records).
//
// Nothing is fsynced at the seal. A kill or a power loss leaves sealed directories behind;
// Recover, run before the caller works out its next block from the cut files, writes the final
// file of every intact one (the same bytes) and drops a torn one (a CRC or size off) with every
// later sealed directory and the open tier0: those blocks are executed again from the source.
//
// Seal file (little-endian): magic "FFT0SEA\x01" | first u64 | ordinal u64 | blocks u32 |
// txs u32 | bodies store bytes u64 | facts spool bytes u64 | facts CRC u32 | per section:
// record bytes u64 | spool CRC u32 (bodies: of the store) | per block: txs before it u32 |
// store offset u64 | per section record offset u32 | CRC-32C of everything before it u32.

const (
	sealMagic   = "FFT0SEA\x01"
	sealName    = "seal"
	sealPrefix  = "sealed."
	finalName   = "final"   // in a sealed directory: the final file's hash, written once it is in place
	maxQueued   = 2         // sealed tier0s waiting for the final writer beyond the one it writes
	sealTmpSufx = ".tmp"    // a seal in progress
	finalTmp    = "t.tier0" // in a sealed directory: the final file being written
)

var errTornSeal = errors.New("tier0: torn sealed tier0")

// hook is called at the kill-test points: "sealed" (sealed directory in place), "encoded"
// (final data file written, not yet renamed) and "final" (final file and sidecar in place,
// the sealed directory not yet released).
var hook = func(point string) {}

// Sealed is a cut tier0 whose final file is written on the side.
type Sealed struct {
	First, Last  uint64
	FirstOrdinal uint64
	Txs          int
	// File reads the sealed records (raw, from the spools) with the read API of the final
	// file; safe for concurrent use, valid until Release.
	File *File
	// FactsPath is the sealed facts sidecar (the bytes of the final <hash>.facts), valid
	// until Release.
	FactsPath string

	sp       *spools
	out      string // the writer's directory: where the final file goes
	done     chan struct{}
	cut      *Cut
	err      error
	release sync.Once
	relErr  error
}

// Done is closed when the final file is written or failed (Wait has the outcome).
func (s *Sealed) Done() <-chan struct{} { return s.done }

// Wait waits for the final file and returns it.
func (s *Sealed) Wait() (*Cut, error) {
	<-s.done
	return s.cut, s.err
}

// Release waits for the final file, closes File and removes the sealed directory; call it
// once nothing reads File or FactsPath any more (switch readers to the final file first). After
// a failed final write the sealed directory stays for Recover.
func (s *Sealed) Release() error {
	<-s.done
	s.release.Do(func() {
		s.File.Close()
		s.relErr = s.sp.close()
		if s.err == nil {
			if err := os.RemoveAll(s.sp.dir); s.relErr == nil {
				s.relErr = err
			}
		}
	})
	return s.relErr
}

// finish is the synchronous cut: the final file, then the release.
func (s *Sealed) finish() (*Cut, error) {
	c, err := s.Wait()
	if rerr := s.Release(); err == nil {
		err = rerr
	}
	return c, err
}

func (w *Writer) err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failed
}

// Seal seals the open tier0 (see Sealed) and starts an empty one. AddSeal seals at CutTxs by
// itself; calling Seal earlier is for tests and fixture runs only. It waits while maxQueued
// sealed tier0s wait for the final writer.
func (w *Writer) Seal() (*Sealed, error) {
	if err := w.err(); err != nil {
		return nil, err
	}
	if w.n == 0 {
		return nil, errors.New("tier0: nothing to cut")
	}
	sp := w.spools
	h := factsHeader(sp.first, sp.n, sp.ordinal)
	if _, err := sp.facts.WriteAt(h, 0); err != nil {
		return nil, err
	}
	final := filepath.Join(w.dir, fmt.Sprintf("%s%020d", sealPrefix, sp.first))
	tmp := final + sealTmpSufx
	os.RemoveAll(tmp)
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return nil, err
	}
	names := []string{bodiesName, factsName}
	for s := range nsec {
		if s != SecBodies {
			names = append(names, SectionNames[s]+".spool")
		}
	}
	for _, n := range names { // open handles follow the rename
		if err := os.Rename(filepath.Join(w.dir, n), filepath.Join(tmp, n)); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, sealName), sp.sealFile(), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return nil, err
	}
	hook("sealed")
	sp.dir = final
	if err := w.openSpools(w.dir, sp.ordinal+uint64(sp.txs), true); err != nil {
		return nil, err
	}
	w.stored = nil
	s := newSealed(&sp, w.dir)
	if w.jobs == nil {
		w.jobs = make(chan *Sealed, maxQueued)
		w.wg.Add(1)
		go w.finalWriter()
	}
	w.jobs <- s
	return s, nil
}

func newSealed(sp *spools, out string) *Sealed {
	return &Sealed{First: sp.first, Last: sp.first + uint64(sp.n) - 1, FirstOrdinal: sp.ordinal, Txs: sp.txs,
		File: sp.view(), FactsPath: filepath.Join(sp.dir, factsName), sp: sp, out: out, done: make(chan struct{})}
}

// finalWriter writes the final files of the sealed tier0s in seal order; after a failure it
// writes no more (a later file would leave a gap in the cut files).
func (w *Writer) finalWriter() {
	defer w.wg.Done()
	for s := range w.jobs {
		if err := w.err(); err != nil {
			s.err = fmt.Errorf("tier0: an earlier final file failed: %w", err)
		} else if s.cut, s.err = s.sp.finalize(s.out); s.err != nil {
			w.mu.Lock()
			w.failed = s.err
			w.mu.Unlock()
		}
		close(s.done)
	}
}

// finalize writes the final file <hash>.tier0 and its sidecar <hash>.facts into out from the
// sealed spools: the sidecar first, so a cut file has its sidecar until the index piece is
// built from it (the pipeline removes it then), both fsynced, then the directory.
func (sp *spools) finalize(out string) (*Cut, error) {
	tmp := filepath.Join(sp.dir, finalTmp)
	hash, err := writeV6(tmp, sp.first, sp.txs, sp.ordinal, sp.txc(), sp.records)
	if err != nil {
		os.Remove(tmp + ".data")
		return nil, err
	}
	hook("encoded")
	name := filepath.Join(out, hex.EncodeToString(hash[:]))
	if err := copyFile(filepath.Join(sp.dir, factsName), name+".facts"); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, name+".tier0"); err != nil {
		return nil, err
	}
	if err := syncDir(out); err != nil {
		return nil, err
	}
	hook("final")
	// the marker spares Recover the encoding when only the release was lost; without it
	// Recover encodes again (the same bytes)
	if err := os.WriteFile(filepath.Join(sp.dir, finalName), []byte(hex.EncodeToString(hash[:])), 0o644); err != nil {
		return nil, err
	}
	return &Cut{Path: name + ".tier0", Hash: hash, First: sp.first, Last: sp.first + uint64(sp.n) - 1, FirstOrdinal: sp.ordinal, Txs: sp.txs}, nil
}

func factsHeader(first uint64, n int, ordinal uint64) []byte {
	h := append([]byte(factsMagic), make([]byte, factsHead-8)...)
	binary.LittleEndian.PutUint64(h[8:], first)
	binary.LittleEndian.PutUint32(h[16:], uint32(n))
	binary.LittleEndian.PutUint64(h[20:], ordinal)
	return h
}

// view is the raw read view of sealed spools: records located by the tables, bodies inside
// the store's records.
func (sp *spools) view() *File {
	f := &File{sealed: true, First: sp.first, FirstOrdinal: sp.ordinal, Blocks: sp.n, Txs: sp.txs,
		cum: append(slices.Clone(sp.cum), uint32(sp.txs))}
	w := &sp.off[SecWrappers]
	for s := range nsec {
		x := &f.sec[s]
		x.off = append(slices.Clone(sp.off[s]), uint32(sp.size[s]))
		x.raw, x.stored = int64(sp.size[s]), int64(sp.size[s])
		if s != SecBodies {
			x.ra = sp.spool[s]
			continue
		}
		x.ra = sp.bodies
		x.pos = make([]int64, sp.n)
		for i := range sp.n {
			wend := uint32(sp.size[SecWrappers])
			if i+1 < sp.n {
				wend = (*w)[i+1]
			}
			x.pos[i] = sp.bodyAt[i] + 16 + int64(wend-(*w)[i]) // see storeRec
		}
	}
	return f
}

func (sp *spools) sealFile() []byte {
	b := []byte(sealMagic)
	b = binary.LittleEndian.AppendUint64(b, sp.first)
	b = binary.LittleEndian.AppendUint64(b, sp.ordinal)
	b = binary.LittleEndian.AppendUint32(b, uint32(sp.n))
	b = binary.LittleEndian.AppendUint32(b, uint32(sp.txs))
	b = binary.LittleEndian.AppendUint64(b, uint64(sp.storeAt))
	b = binary.LittleEndian.AppendUint64(b, uint64(sp.factsAt))
	b = binary.LittleEndian.AppendUint32(b, sp.factsCRC)
	for s := range nsec {
		b = binary.LittleEndian.AppendUint64(b, sp.size[s])
		b = binary.LittleEndian.AppendUint32(b, sp.crc[s])
	}
	for i := range sp.n {
		b = binary.LittleEndian.AppendUint32(b, sp.cum[i])
		b = binary.LittleEndian.AppendUint64(b, uint64(sp.bodyAt[i]))
		for s := range nsec {
			b = binary.LittleEndian.AppendUint32(b, sp.off[s][i])
		}
	}
	return binary.LittleEndian.AppendUint32(b, crc32.Checksum(b, crcTab))
}

// loadSealed opens a sealed directory and checks every file against its seal file
// (errTornSeal when one does not match).
func loadSealed(dir string) (*spools, error) {
	b, err := os.ReadFile(filepath.Join(dir, sealName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errTornSeal
	}
	if err != nil {
		return nil, err
	}
	const fixed = 8 + 8 + 8 + 4 + 4 + 8 + 8 + 4 + nsec*12
	if len(b) < fixed+4 || string(b[:8]) != sealMagic || crc32.Checksum(b[:len(b)-4], crcTab) != binary.LittleEndian.Uint32(b[len(b)-4:]) {
		return nil, errTornSeal
	}
	sp := &spools{dir: dir}
	c := b[8:]
	u64 := func() uint64 { v := binary.LittleEndian.Uint64(c); c = c[8:]; return v }
	u32 := func() uint32 { v := binary.LittleEndian.Uint32(c); c = c[4:]; return v }
	sp.first, sp.ordinal, sp.n, sp.txs = u64(), u64(), int(u32()), int(u32())
	sp.storeAt, sp.factsAt, sp.factsCRC = int64(u64()), int64(u64()), u32()
	for s := range nsec {
		sp.size[s], sp.crc[s] = u64(), u32()
	}
	if sp.n == 0 || len(b) != fixed+4+sp.n*(12+4*nsec) {
		return nil, errTornSeal
	}
	sp.cum, sp.bodyAt = make([]uint32, sp.n), make([]int64, sp.n)
	for s := range nsec {
		sp.off[s] = make([]uint32, sp.n)
	}
	for i := range sp.n {
		sp.cum[i], sp.bodyAt[i] = u32(), int64(u64())
		for s := range nsec {
			sp.off[s][i] = u32()
		}
	}
	ok := false
	defer func() {
		if !ok {
			sp.close()
		}
	}()
	if sp.bodies, err = checkSpool(filepath.Join(dir, bodiesName), 0, sp.storeAt, sp.crc[SecBodies]); err != nil {
		return nil, err
	}
	for s := range nsec {
		if s != SecBodies {
			if sp.spool[s], err = checkSpool(filepath.Join(dir, SectionNames[s]+".spool"), 0, int64(sp.size[s]), sp.crc[s]); err != nil {
				return nil, err
			}
		}
	}
	if sp.facts, err = checkSpool(filepath.Join(dir, factsName), factsHead, sp.factsAt, sp.factsCRC); err != nil {
		return nil, err
	}
	h := make([]byte, factsHead)
	if _, err := sp.facts.ReadAt(h, 0); err != nil || !bytes.Equal(h, factsHeader(sp.first, sp.n, sp.ordinal)) {
		return nil, errTornSeal
	}
	ok = true
	return sp, nil
}

// checkSpool opens a sealed file and checks its size and the CRC-32C of its bytes from skip.
func checkSpool(path string, skip, size int64, crc uint32) (*os.File, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errTornSeal
	}
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil && st.Size() != size {
		err = errTornSeal
	}
	if err == nil {
		h := crc32.New(crcTab)
		if _, err = io.Copy(h, io.NewSectionReader(f, skip, math.MaxInt64)); err == nil && h.Sum32() != crc {
			err = errTornSeal
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// sealedDirs lists the sealed directories in dir (seals in progress included) by first block.
func sealedDirs(dir string) ([]string, error) {
	m, err := filepath.Glob(filepath.Join(dir, sealPrefix+"*"))
	slices.Sort(m) // fixed-width block numbers: name order is block order, a seal in progress after its sealed name
	return m, err
}

// Recover finishes what a killed writer left in dir; run it before working out the next block
// from the cut files. It writes the final file of every intact sealed tier0, in order (the
// bytes a synchronous cut gives), and removes the sealed directories. From the first torn one
// on (a seal in progress, a size or CRC that does not match its seal file) it removes the
// sealed directories and the open tier0's bodies store: those blocks are executed again from
// the source. Returns the final files it wrote or found in place.
func Recover(dir string) ([]*Cut, error) {
	ds, err := sealedDirs(dir)
	if err != nil {
		return nil, err
	}
	var cuts []*Cut
	torn := false
	for _, d := range ds {
		if !torn {
			c, err := recoverOne(d, dir)
			switch {
			case errors.Is(err, errTornSeal):
				torn = true
			case err != nil:
				return cuts, fmt.Errorf("tier0: recover %s: %w", filepath.Base(d), err)
			default:
				cuts = append(cuts, c)
			}
		}
		if err := os.RemoveAll(d); err != nil {
			return cuts, err
		}
	}
	if torn {
		if err := os.Remove(filepath.Join(dir, bodiesName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return cuts, err
		}
	}
	return cuts, syncDir(dir)
}

// recoverOne writes the final file of one sealed directory (errTornSeal: it cannot).
func recoverOne(d, out string) (*Cut, error) {
	if strings.HasSuffix(d, sealTmpSufx) {
		return nil, errTornSeal
	}
	if _, err := strconv.ParseUint(strings.TrimPrefix(filepath.Base(d), sealPrefix), 10, 64); err != nil {
		return nil, fmt.Errorf("not a sealed directory")
	}
	sp, err := loadSealed(d)
	if err != nil {
		return nil, err
	}
	defer sp.close()
	if c, ok := sp.finalInPlace(out); ok {
		return c, nil
	}
	return sp.finalize(out)
}

// finalInPlace reports the final file named by the sealed directory's marker when it is in out
// with the right hash. Its sidecar is not checked: the marker is written after the dir fsync
// that made both durable, and the sidecar is removed once the file's index piece is built.
func (sp *spools) finalInPlace(out string) (*Cut, bool) {
	m, err := os.ReadFile(filepath.Join(sp.dir, finalName))
	if err != nil {
		return nil, false
	}
	hb, err := hex.DecodeString(string(m))
	if err != nil || len(hb) != 32 {
		return nil, false
	}
	name := filepath.Join(out, string(m))
	f, err := Open(name+".tier0", chunkfile.Hash(hb))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if f.First != sp.first || f.Blocks != sp.n || f.Txs != sp.txs || f.FirstOrdinal != sp.ordinal {
		return nil, false
	}
	return &Cut{Path: name + ".tier0", Hash: chunkfile.Hash(hb), First: sp.first, Last: sp.first + uint64(sp.n) - 1, FirstOrdinal: sp.ordinal, Txs: sp.txs}, true
}
