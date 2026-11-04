package dist

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// Files is what Serve answers from: one shared *chunkfile.File per hash
// (a *Dir, or Dirs for a node whose files sit in several directories).
type Files interface {
	File(hash Hash) (*chunkfile.File, error)
}

// Serve registers the HTTP language on mux, answering from the chunk files
// of d:
//
//	GET /{hash}           raw file bytes, one Range or none
//	GET /{hash}/announce  Version byte + zstd presence bitmap
//
// It serves only chunks it holds, verified, and never fetches on another's
// behalf. Status codes are in the package doc.
func Serve(mux *http.ServeMux, d Files) {
	mux.HandleFunc("GET /{hash}", func(w http.ResponseWriter, r *http.Request) { serveFile(w, r, d) })
	mux.HandleFunc("GET /{hash}/announce", func(w http.ResponseWriter, r *http.Request) { serveAnnounce(w, r, d) })
}

func lookup(w http.ResponseWriter, r *http.Request, d Files) *chunkfile.File {
	var h Hash
	s := r.PathValue("hash")
	if len(s) != 2*len(h) {
		http.NotFound(w, r)
		return nil
	}
	if _, err := hex.Decode(h[:], []byte(s)); err != nil {
		http.NotFound(w, r)
		return nil
	}
	f, err := d.File(h)
	if errors.Is(err, ErrUnknown) {
		http.NotFound(w, r)
		return nil
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	return f
}

func serveAnnounce(w http.ResponseWriter, r *http.Request, d Files) {
	f := lookup(w, r, d)
	if f == nil {
		return
	}
	msg := Presence(f).Announce()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(msg)))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(msg)
}

// parseRange reads the one form the language allows, "bytes=a-b",
// "bytes=a-" or "bytes=-k", against a file of size bytes, and returns the
// inclusive range clipped to the file. ok is false for anything else, a
// start past the end and an empty suffix.
func parseRange(h string, size int64) (a, b int64, ok bool) {
	spec, found := strings.CutPrefix(h, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	first, last, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false
	}
	if first == "" {
		k, err := strconv.ParseInt(last, 10, 64)
		if err != nil || k <= 0 {
			return 0, 0, false
		}
		return max(0, size-k), size - 1, true
	}
	a, err := strconv.ParseInt(first, 10, 64)
	if err != nil || a < 0 || a >= size {
		return 0, 0, false
	}
	b = size - 1
	if last != "" {
		if b, err = strconv.ParseInt(last, 10, 64); err != nil || b < a {
			return 0, 0, false
		}
		b = min(b, size-1)
	}
	return a, b, true
}

func serveFile(w http.ResponseWriter, r *http.Request, d Files) {
	f := lookup(w, r, d)
	if f == nil {
		return
	}
	size := f.Size()
	a, b, status := int64(0), size-1, http.StatusOK
	refuse := func() {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		http.Error(w, "range not served", http.StatusRequestedRangeNotSatisfiable)
	}
	if h := r.Header.Get("Range"); h != "" {
		var ok bool
		if a, b, ok = parseRange(h, size); !ok {
			refuse()
			return
		}
		status = http.StatusPartialContent
	}
	// Pin every chunk of the range: eviction cannot punch it mid-answer.
	ci, cj := a/chunkfile.ChunkSize, b/chunkfile.ChunkSize
	for i := ci; i <= cj; i++ {
		if err := f.Acquire(i); err != nil {
			for k := ci; k < i; k++ {
				f.Release(k)
			}
			refuse()
			return
		}
	}
	defer func() {
		for i := ci; i <= cj; i++ {
			f.Release(i)
		}
	}()
	hd := w.Header()
	hd.Set("Content-Type", "application/octet-stream")
	hd.Set("Accept-Ranges", "bytes")
	hd.Set("Content-Length", strconv.FormatInt(b-a+1, 10))
	if status == http.StatusPartialContent {
		hd.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, size))
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	for i := ci; i <= cj; i++ {
		k, err := f.ReadChunk(i, *bp)
		if err != nil {
			// The chunk is pinned, so only an IO error gets here. Cut the
			// connection so the client sees a short body.
			panic(http.ErrAbortHandler)
		}
		s, _ := f.Chunk(i)
		lo, hi := max(a, s)-s, min(b+1, s+int64(k))-s
		if _, err := w.Write((*bp)[lo:hi]); err != nil {
			return
		}
	}
}
