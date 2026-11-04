package dist

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// HTTPCap is the default glue cap of an HTTP source: 16 chunks (16 MiB) per
// request. Why 16: a request costs one round trip and, at R2, $0.36 per
// million; 16 MiB per request makes the 57 GB snapshot 3,500 requests
// instead of 57,000 and keeps the round trip under 10% of the transfer down
// to ~100 MB/s per connection at 100 ms RTT, while a failed or slow request
// wastes at most 16 MiB and the fetcher still streams it through one 1 MiB
// buffer. Larger caps stop paying once requests per second are in the tens.
const HTTPCap = 16

// HTTPSource fetches over HTTP: GET {Base}/{hex hash} with a byte Range (the
// language every source and mirror speaks, see the package doc).
type HTTPSource struct {
	Base    string // e.g. "https://mirror.example/c" (no trailing slash)
	Client  *http.Client
	Cap     int64         // chunks per request
	Retries int           // attempts after the first on 5xx, 429 and network errors
	Backoff time.Duration // first retry delay, doubled per retry, +-25% jitter
	Idle    time.Duration // a body that delivers nothing for this long is cut
}

// NewHTTPSource returns a source with the defaults: cap 16, 4 retries from
// 100 ms (100+200+400+800 ms), 30 s idle cut, 64 idle connections per host
// (Go's default of 2 would reconnect for every request past 2 fetchers),
// and HTTP/1.1 only. HTTP/2 would carry every request to a host on one TCP
// connection: one congestion window for all fetchers (one ssh stream gave
// 15-17 MB/s where eight gave 53-54 MB/s, 2026-10-22), and a walk run
// paused for a fault still receives its stream window (4 MiB in Go) ahead
// of the fault on the shared connection. Separate connections let TCP flow
// control stop a paused run without touching the others.
func NewHTTPSource(base string) *HTTPSource {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ForceAttemptHTTP2 = false
	t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	t.MaxIdleConnsPerHost = 64
	t.ResponseHeaderTimeout = 30 * time.Second
	return &HTTPSource{
		Base:    strings.TrimRight(base, "/"),
		Client:  &http.Client{Transport: t},
		Cap:     HTTPCap,
		Retries: 4,
		Backoff: 100 * time.Millisecond,
		Idle:    30 * time.Second,
	}
}

func (s *HTTPSource) String() string   { return s.Base }
func (s *HTTPSource) MaxChunks() int64 { return s.Cap }

// retryable reports whether a failed attempt may succeed when repeated.
func retryable(ctx context.Context, err error, code int) bool {
	if ctx.Err() != nil {
		return false
	}
	if err != nil {
		var ne net.Error
		return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
			errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED)
	}
	return code >= 500 || code == http.StatusTooManyRequests
}

// do sends GET url (with rng as the Range header when set) and retries what
// may succeed when repeated. The response is returned for any final status.
func (s *HTTPSource) do(ctx context.Context, url, rng string) (*http.Response, context.CancelFunc, error) {
	delay := s.Backoff
	for attempt := 0; ; attempt++ {
		rctx, cancel := context.WithCancel(ctx)
		req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			return nil, nil, err
		}
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := s.Client.Do(req)
		code := 0
		if err == nil {
			code = resp.StatusCode
			if !retryable(ctx, nil, code) || attempt >= s.Retries {
				return resp, cancel, nil
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
		}
		cancel()
		if err != nil && (!retryable(ctx, err, 0) || attempt >= s.Retries) {
			return nil, nil, err
		}
		j := time.Duration(float64(delay) * (0.75 + 0.5*rand.Float64()))
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(j):
		}
		delay *= 2
	}
}

func (s *HTTPSource) Fetch(ctx context.Context, hash Hash, first, n int64) (io.ReadCloser, error) {
	if n < 1 || n > s.Cap {
		return nil, fmt.Errorf("dist: fetch of %d chunks, cap %d", n, s.Cap)
	}
	a, b := first*chunkfile.ChunkSize, (first+n)*chunkfile.ChunkSize-1
	resp, cancel, err := s.do(ctx, s.Base+"/"+hex.EncodeToString(hash[:]), fmt.Sprintf("bytes=%d-%d", a, b))
	if err != nil {
		return nil, err
	}
	fail := func(err error) (io.ReadCloser, error) {
		resp.Body.Close()
		cancel()
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusNotFound:
		return fail(ErrUnknown)
	case http.StatusRequestedRangeNotSatisfiable:
		return fail(ErrNotHeld)
	default:
		return fail(fmt.Errorf("dist: %s: status %s", s.Base, resp.Status))
	}
	// The body must start at a and end at b, or earlier only at the end of
	// the file: "bytes a-e/size" with e == b or e == size-1.
	var ga, ge, size int64
	if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &ga, &ge, &size); err != nil {
		return fail(fmt.Errorf("dist: %s: Content-Range %q", s.Base, resp.Header.Get("Content-Range")))
	}
	if ga != a || ge < ga || ge > b || (ge != b && ge != size-1) {
		return fail(fmt.Errorf("dist: %s: asked %d-%d, got %d-%d/%d", s.Base, a, b, ga, ge, size))
	}
	if resp.ContentLength >= 0 && resp.ContentLength != ge-ga+1 {
		return fail(fmt.Errorf("dist: %s: Content-Length %d for %d-%d", s.Base, resp.ContentLength, ga, ge))
	}
	return newIdleBody(resp.Body, ge-ga+1, s.Idle, cancel), nil
}

// idleBody cuts a body that stalls for idle, and reports a short body as
// io.ErrUnexpectedEOF.
type idleBody struct {
	rc     io.ReadCloser
	left   int64
	idle   time.Duration
	t      *time.Timer
	cancel context.CancelFunc
}

func newIdleBody(rc io.ReadCloser, n int64, idle time.Duration, cancel context.CancelFunc) *idleBody {
	t := time.AfterFunc(idle, cancel)
	t.Stop()
	return &idleBody{rc: rc, left: n, idle: idle, t: t, cancel: cancel}
}

// Read arms the idle timer only while it waits: time the reader spends away
// (a walk run paused for faults) is not the source stalling.
func (b *idleBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	b.t.Reset(b.idle)
	n, err := b.rc.Read(p)
	b.t.Stop()
	b.left -= int64(n)
	if err == io.EOF && b.left > 0 {
		err = io.ErrUnexpectedEOF
	}
	if b.left == 0 && err == io.EOF {
		err = nil
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.t.Stop()
	err := b.rc.Close()
	b.cancel()
	return err
}

func (s *HTTPSource) Have(ctx context.Context, hash Hash, n int64) (*Bitmap, error) {
	resp, cancel, err := s.do(ctx, s.Base+"/"+hex.EncodeToString(hash[:])+"/announce", "")
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// A mirror has no announce object; a node without the file also
		// answers 404, and Fetch then says ErrUnknown.
		return nil, ErrNoAnnounce
	default:
		return nil, fmt.Errorf("dist: %s announce: status %s", s.Base, resp.Status)
	}
	msg, err := io.ReadAll(io.LimitReader(resp.Body, maxAnnounce+1))
	if err != nil {
		return nil, err
	}
	return ParseAnnounce(msg, n)
}
