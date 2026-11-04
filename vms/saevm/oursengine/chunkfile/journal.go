package chunkfile

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The crash journal of a chunk file, "<path>.wal". Presence is trusted from
// the filesystem; the only chunks that can be present with wrong bytes are
// those being written when the machine died. So before a run of chunks is
// written, "begin first,n" is appended and fsynced; at Open every begun run
// is a suspect and its present chunks are hashed (a mismatch is punched).
//
// There are no "done" records: a checkpoint fsyncs the data file and then
// rewrites the journal to the runs not covered by that fsync (or truncates
// it when there are none), the same guarantee with a journal no bigger than
// the runs in flight. A checkpoint starts in the background from a run's
// end at most once per checkpointEvery or after maxRecords records, and at
// Sync and Close; the data fsync never holds up writers. Runs ended since
// the last checkpoint stay suspects until the next: a crash costs hashing
// about a second of writes.
//
// One journal per file (not per directory): chunkfile.Open needs nothing
// but the path, and unrelated files never share fsyncs or recovery.
//
// Files made by Create are complete by construction (fsync, then rename)
// and have no journal; only Write (downloads) journals.

const (
	recSize         = 1 + 8 + 8 + 4 // 'B', first, n, crc32c of the 17 bytes before
	checkpointEvery = time.Second
	maxRecords      = 4096
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

type run struct{ first, n int64 }

type journal struct {
	path string
	data *os.File
	ckMu sync.Mutex // one checkpoint at a time
	mu   sync.Mutex // the fields below; never held across a data fsync
	f    *os.File   // nil while the journal file is absent
	open map[*run]struct{}
	// ended: runs whose writes are done but not yet covered by a data
	// fsync; they stay in the journal until a checkpoint covers them.
	ended  []*run
	logged int // records in the file
	last   time.Time
	ckBusy bool
	err    error // a failed checkpoint; returned by the next begin
}

func newJournal(path string, data *os.File) *journal {
	return &journal{path: path + ".wal", data: data, open: map[*run]struct{}{}, last: time.Now()}
}

func encodeRec(r run) []byte {
	b := make([]byte, recSize)
	b[0] = 'B'
	binary.LittleEndian.PutUint64(b[1:], uint64(r.first))
	binary.LittleEndian.PutUint64(b[9:], uint64(r.n))
	binary.LittleEndian.PutUint32(b[17:], crc32.Checksum(b[:17], castagnoli))
	return b
}

// readJournal returns the runs recorded in the journal at path. A record
// that is short or fails its crc ends the list: it was never fsynced, so no
// data write followed it.
func readJournal(path string) ([]run, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []run
	for ; len(b) >= recSize; b = b[recSize:] {
		if b[0] != 'B' || binary.LittleEndian.Uint32(b[17:]) != crc32.Checksum(b[:17], castagnoli) {
			break
		}
		runs = append(runs, run{int64(binary.LittleEndian.Uint64(b[1:])), int64(binary.LittleEndian.Uint64(b[9:]))})
	}
	return runs, nil
}

// begin records r durably before any of its bytes are written.
func (j *journal) begin(r *run) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err != nil {
		return j.err
	}
	if j.f == nil {
		f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		if err := syncDir(j.path); err != nil {
			f.Close()
			return err
		}
		j.f = f
	}
	if _, err := j.f.Write(encodeRec(*r)); err != nil {
		return err
	}
	if err := j.f.Sync(); err != nil {
		return err
	}
	j.open[r] = struct{}{}
	j.logged++
	return nil
}

// end closes r and starts a checkpoint in the background when one is due,
// so neither the caller nor other writers wait for the data fsync.
func (j *journal) end(r *run) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.open, r)
	j.ended = append(j.ended, r)
	if !j.ckBusy && (time.Since(j.last) >= checkpointEvery || j.logged >= maxRecords) {
		j.ckBusy = true
		go j.checkpoint()
	}
	return nil
}

func (j *journal) covers(i int64) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for r := range j.open {
		if i >= r.first && i < r.first+r.n {
			return true
		}
	}
	return false
}

// checkpoint makes the runs ended so far durable in the data file and drops
// them from the journal. The data fsync runs without j.mu, so writers keep
// beginning runs meanwhile; runs ended during it stay for the next one.
func (j *journal) checkpoint() error {
	j.ckMu.Lock()
	defer j.ckMu.Unlock()
	j.mu.Lock()
	drop := j.ended
	j.ended = nil
	if j.f == nil || len(drop) == 0 {
		j.last, j.ckBusy = time.Now(), false
		j.mu.Unlock()
		return nil
	}
	j.mu.Unlock()
	err := j.data.Sync()
	j.mu.Lock()
	defer j.mu.Unlock()
	j.last, j.ckBusy = time.Now(), false
	if err == nil {
		err = j.rewriteLocked()
	}
	if err != nil {
		j.ended = append(drop, j.ended...)
		j.err = err
	}
	return err
}

// rewriteLocked replaces the journal with the open and ended runs: an
// atomic rename, or a truncate when there are none.
func (j *journal) rewriteLocked() error {
	keep := len(j.open) + len(j.ended)
	if keep == 0 {
		if err := j.f.Truncate(0); err != nil {
			return err
		}
		if err := j.f.Sync(); err != nil {
			return err
		}
		j.logged = 0
		return nil
	}
	tmp := j.path + ".tmp"
	t, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	buf := make([]byte, 0, keep*recSize)
	for r := range j.open {
		buf = append(buf, encodeRec(*r)...)
	}
	for _, r := range j.ended {
		buf = append(buf, encodeRec(*r)...)
	}
	if _, err := t.Write(buf); err != nil {
		t.Close()
		return err
	}
	if err := t.Sync(); err != nil {
		t.Close()
		return err
	}
	t.Close()
	if err := os.Rename(tmp, j.path); err != nil {
		return err
	}
	if err := syncDir(j.path); err != nil {
		return err
	}
	j.f.Close()
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		j.f = nil
		return err
	}
	j.f = f
	j.logged = keep
	return nil
}

// close checkpoints and removes an empty journal.
func (j *journal) close() error {
	err := j.checkpoint()
	j.ckMu.Lock()
	defer j.ckMu.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return err
	}
	if err == nil && j.logged == 0 {
		err = errors.Join(os.Remove(j.path), syncDir(j.path))
	}
	err = errors.Join(err, j.f.Close())
	j.f = nil
	return err
}

func syncDir(path string) error {
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// recover settles the suspects a crash left: every present chunk of a run
// in the journal is hashed and punched on mismatch; then the punches are
// fsynced and the journal removed.
func (cf *File) recover(path string) error {
	os.Remove(path + ".wal.tmp") // a checkpoint that died before its rename
	runs, err := readJournal(path + ".wal")
	if err != nil || len(runs) == 0 {
		if err == nil {
			err = removeJournal(path)
		}
		return err
	}
	bp := chunkPool.Get().(*[]byte)
	defer chunkPool.Put(bp)
	for _, r := range runs {
		for i := max(r.first, cf.h); i < min(r.first+r.n, cf.n); i++ {
			if !cf.Present(i) {
				continue
			}
			cf.Suspects++
			s, e := cf.Chunk(i)
			b := (*bp)[:e-s]
			if _, err := cf.f.ReadAt(b, s); err != nil && err != io.EOF {
				return err
			}
			if cf.Verify(i, b) == nil {
				continue
			}
			cf.present[i/64].And(^(uint64(1) << (i % 64)))
			if err := punch(cf.f, s, e-s); err != nil {
				return err
			}
			cf.Punched++
		}
	}
	if err := cf.f.Sync(); err != nil {
		return err
	}
	return removeJournal(path)
}

func removeJournal(path string) error {
	err := os.Remove(path + ".wal")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDir(path)
}
