package chunkfile

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// SealStep is the data bytes Seal copies between checkpoints (a var for tests); sealHook, when
// set (tests), runs after each checkpoint.
var (
	SealStep int64 = 64 << 20
	sealHook func(done int64)
)

// Seal makes the chunk file of the data held in the file at data, for data too big to hold
// twice on disk. The data starts at the first chunk after the hash list (SealLayout): the list
// has the first H chunks to itself, zero padded, so the copy is chunk aligned and a reflink
// where the filesystem has them (copy_file_range: XFS, no bytes copied). It copies the data in
// SealStep pieces into data+".part" at that offset, fsyncs, records the progress in
// data+".seal" and punches the copied piece out of data; then hashes the chunks (in parallel),
// writes the list and the pad, fsyncs and renames the result to dir/<hash hex><ext>, and
// removes data and the state. A crash anywhere resumes: call Seal again with the same
// arguments; a seal state written before the layout (no offset in it) resumes in the layout
// it began, the data right after the list as Create and Finish write it. Extra disk: one step
// (none with reflinks).
func Seal(data, dir, ext string) (path string, hash Hash, size int64, err error) {
	return SealWith(data, dir, ext, 0, nil, nil)
}

// SealLayout is the layout Seal gives d data bytes: n chunks, the first h of them the hash
// list and zeros, the data from off = h*ChunkSize on. The chunk contract is the usual one
// (Layout(off+d) is n, h); only the data part begins with off-32*(n-h) zero bytes.
func SealLayout(d int64) (n, h, off int64) {
	dc := (d + ChunkSize - 1) / ChunkSize
	// headerChunks(h+dc)-h starts >= 0 and drops by 0 or 1 per step: it reaches 0.
	for h = 1; ; h++ {
		if n = h + dc; headerChunks(n) == h {
			return n, h, h * ChunkSize
		}
	}
}

// SealWith is Seal of the data the file at data holds from offset from on (the bytes before
// it are never read; a resume passes the same from) that tells copied, before it punches
// anything out of data, that the data bytes [0, n) are durable in data+".part" at offset off
// (n = all of it once sealed): a reader of the data while it moves reads below n from the
// part file. sums, when not nil, are the
// sha256 of the data's ChunkSize pieces in order (Sums: hashed as the data was written); they
// are the hash list in SealLayout (data piece i is chunk H+i), so the seal reads nothing back.
// A seal begun before SealLayout hashes its chunks as before and ignores sums.
func SealWith(data, dir, ext string, from int64, sums []Hash, copied func(off, n int64)) (path string, hash Hash, size int64, err error) {
	if copied == nil {
		copied = func(int64, int64) {}
	}
	state, part := data+".seal", data+".part"
	done, hash, sealed, d, off, err := readSeal(state)
	if err != nil {
		return "", hash, 0, err
	}
	if sealed {
		path = filepath.Join(dir, hex.EncodeToString(hash[:])+ext)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(part, path); err != nil {
				return "", hash, 0, err
			}
			if err := syncDir(path); err != nil {
				return "", hash, 0, err
			}
		}
		st, err := os.Stat(path)
		if err != nil {
			return "", hash, 0, err
		}
		return path, hash, st.Size(), removeSeal(data, state)
	}
	src, err := os.OpenFile(data, os.O_RDWR, 0)
	if err != nil {
		return "", hash, 0, err
	}
	defer src.Close()
	if done == 0 {
		st, err := src.Stat()
		if err != nil {
			return "", hash, 0, err
		}
		if d = st.Size() - from; d < 0 {
			return "", hash, 0, fmt.Errorf("chunkfile: %s is %d B, its data starts at %d", data, st.Size(), from)
		}
	}
	var n, h int64
	if off < 0 { // a seal begun before SealLayout: the data right after the list
		var hdr int64
		n, h, hdr = dataLayout(d)
		off, sums = hdr, nil
	} else {
		n, h, off = SealLayout(d)
		if sums != nil && int64(len(sums)) != n-h {
			return "", hash, 0, fmt.Errorf("chunkfile: %d chunk sums for %d B of data, want %d", len(sums), d, n-h)
		}
	}
	size = off + d
	flag := os.O_RDWR | os.O_CREATE
	if done == 0 {
		flag |= os.O_TRUNC
	} else if _, err := os.Stat(part); err != nil {
		return "", hash, 0, fmt.Errorf("chunkfile: seal state says %d B copied but %s is gone: %w", done, part, err)
	}
	out, err := os.OpenFile(part, flag, 0o644)
	if err != nil {
		return "", hash, 0, err
	}
	defer out.Close()
	if done == 0 {
		NoPrealloc(out)
	}
	copied(off, done)
	for x := done; x < d; {
		k := min(SealStep, d-x)
		if _, err := out.Seek(off+x, io.SeekStart); err != nil {
			return "", hash, 0, err
		}
		if _, err := src.Seek(from+x, io.SeekStart); err != nil {
			return "", hash, 0, err
		}
		c, err := out.ReadFrom(io.LimitReader(src, k))
		if err == nil && c != k {
			err = fmt.Errorf("chunkfile: seal copied %d of %d B", c, k)
		}
		if err != nil {
			return "", hash, 0, err
		}
		if err := out.Sync(); err != nil {
			return "", hash, 0, err
		}
		if err := writeSeal(state, x+k, d, off, nil); err != nil {
			return "", hash, 0, err
		}
		copied(off, x+k)
		if err := punch(src, from+x, k); err != nil {
			return "", hash, 0, err
		}
		x += k
		if sealHook != nil {
			sealHook(x)
		}
	}
	head := make([]byte, off) // the list, then zeros to the data
	list := head[:HashSize*(n-h)]
	for i, s := range sums {
		copy(list[i*HashSize:], s[:])
	}
	todo := h // the first chunk to hash
	if sums != nil {
		todo = n
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	next := make(chan int64)
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bp := chunkPool.Get().(*[]byte)
			defer chunkPool.Put(bp)
			for i := range next {
				s, e := i*ChunkSize, min((i+1)*ChunkSize, size)
				if _, err := out.ReadAt((*bp)[:e-s], s); err != nil {
					mu.Lock()
					first = errors.Join(first, err)
					mu.Unlock()
					continue
				}
				sum := sha256.Sum256((*bp)[:e-s])
				copy(list[(i-h)*HashSize:], sum[:])
			}
		}()
	}
	for i := todo; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	if first != nil {
		return "", hash, 0, first
	}
	if _, err := out.WriteAt(head, 0); err != nil {
		return "", hash, 0, err
	}
	if err := out.Sync(); err != nil {
		return "", hash, 0, err
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, io.NewSectionReader(out, 0, min(size, h*ChunkSize))); err != nil {
		return "", hash, 0, err
	}
	hash = Hash(sum.Sum(nil))
	if err := writeSeal(state, d, d, off, &hash); err != nil {
		return "", hash, 0, err
	}
	if sealHook != nil {
		sealHook(d + 1) // sealed, not renamed
	}
	path = filepath.Join(dir, hex.EncodeToString(hash[:])+ext)
	if err := os.Rename(part, path); err != nil {
		return "", hash, 0, err
	}
	if err := syncDir(path); err != nil {
		return "", hash, 0, err
	}
	return path, hash, size, removeSeal(data, state)
}

// SealAt seals in place the data the file at data holds from SealLayout's offset on (the
// bytes before it unused): it writes the hash list (sums, as SealWith takes them: they give
// the data's chunk count, so the layout) and the zero pad in front, fsyncs and renames the
// file to dir/<hash hex><ext>, the bytes Seal makes. No data byte moves, so the file keeps its
// inode and the data's cached pages. A crash anywhere resumes: call SealAt again with the
// same sums (once renamed, the file is found by the hash the sums give).
func SealAt(data, dir, ext string, sums []Hash) (path string, hash Hash, size int64, err error) {
	dc := int64(len(sums))
	if dc == 0 {
		return "", hash, 0, errors.New("chunkfile: SealAt of no data")
	}
	n, h, off := SealLayout(dc * ChunkSize) // the same for every data size of dc chunks
	head := make([]byte, off)               // the list, then zeros to the data
	for i, s := range sums {
		copy(head[i*HashSize:], s[:])
	}
	hash = sha256.Sum256(head) // the first h chunks: the data starts after them
	path = filepath.Join(dir, hex.EncodeToString(hash[:])+ext)
	fits := func(size int64) error {
		if d := size - off; d <= (dc-1)*ChunkSize || d > dc*ChunkSize {
			return fmt.Errorf("chunkfile: %d B with %d chunk sums: the data is not at %d (%d chunks, H %d)", size, dc, off, n, h)
		}
		return nil
	}
	f, err := os.OpenFile(data, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) { // renamed by a call that crashed after
		st, serr := os.Stat(path)
		if serr != nil {
			return "", hash, 0, fmt.Errorf("chunkfile: neither %s nor its sealed file: %w", data, serr)
		}
		if err := fits(st.Size()); err != nil {
			return "", hash, 0, err
		}
		return path, hash, st.Size(), syncDir(path)
	}
	if err != nil {
		return "", hash, 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", hash, 0, err
	}
	if err := fits(st.Size()); err != nil {
		return "", hash, 0, err
	}
	if _, err := f.WriteAt(head, 0); err != nil {
		return "", hash, 0, err
	}
	if err := f.Sync(); err != nil {
		return "", hash, 0, err
	}
	if sealHook != nil {
		sealHook(st.Size() - off) // the head is durable, not renamed
	}
	if err := os.Rename(data, path); err != nil {
		return "", hash, 0, err
	}
	if err := syncDir(path); err != nil {
		return "", hash, 0, err
	}
	if sealHook != nil {
		sealHook(st.Size() - off + 1) // renamed
	}
	return path, hash, st.Size(), nil
}

func removeSeal(data, state string) error {
	for _, p := range []string{data, state} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

var sealTable = crc32.MakeTable(crc32.Castagnoli)

// Seal state: copied u64 | data size u64 | data offset u64 | [hash 32] | crc32c. Absent:
// nothing copied (off is then SealLayout's). A state without the offset (16 or 48 B before the
// crc) is a seal begun before SealLayout: off is -1, the data goes right after the list.
func readSeal(path string) (done int64, hash Hash, sealed bool, d, off int64, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, hash, false, 0, 0, nil
	}
	if err != nil {
		return 0, hash, false, 0, 0, err
	}
	n := len(b) - 4
	if n < 0 || (n != 16 && n != 24 && n != 16+HashSize && n != 24+HashSize) || crc32.Checksum(b[:n], sealTable) != binary.LittleEndian.Uint32(b[n:]) {
		return 0, hash, false, 0, 0, fmt.Errorf("chunkfile: corrupt seal state %s", path)
	}
	fixed := 24
	off = -1
	if n == 16 || n == 16+HashSize {
		fixed = 16
	} else {
		off = int64(binary.LittleEndian.Uint64(b[16:]))
	}
	if n > fixed {
		hash, sealed = Hash(b[fixed:n]), true
	}
	return int64(binary.LittleEndian.Uint64(b)), hash, sealed, int64(binary.LittleEndian.Uint64(b[8:])), off, nil
}

func writeSeal(path string, done, d, off int64, hash *Hash) error {
	b := binary.LittleEndian.AppendUint64(nil, uint64(done))
	b = binary.LittleEndian.AppendUint64(b, uint64(d))
	b = binary.LittleEndian.AppendUint64(b, uint64(off))
	if hash != nil {
		b = append(b, hash[:]...)
	}
	b = binary.LittleEndian.AppendUint32(b, crc32.Checksum(b, sealTable))
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(path)
}
