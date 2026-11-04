package dist

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/oursengine/chunkfile"
)

// Options tune a Downloader. Zero values take the defaults.
type Options struct {
	// Fetchers walk the file in the background and take faults first
	// (default 8).
	Fetchers int
	// FaultWorkers only serve faults, so a fault never waits for a walk
	// fetch to finish (default 4).
	FaultWorkers int
	// OnDemand turns the background walk off: only faults are fetched (files
	// the node does not keep whole).
	OnDemand bool
	// Seed fixes the random walk order (0 = random).
	Seed uint64
	// MaxFailures (K): a source that sent K bad chunks is dropped for good;
	// K transport failures in a row bench it, 1 s doubling to 60 s, and a
	// success resets the count (default 3).
	MaxFailures int
	// Refresh is how often announces are re-read (default 30 s).
	Refresh time.Duration
	// WalkPause bounds how long an in-flight walk run stays paused while
	// faults are fetched before it is abandoned (its chunks go back to the
	// walk, no failure counted). Default 5 s.
	WalkPause time.Duration
}

var (
	// ErrNoSources: every source was dropped (bad bytes or file unknown).
	ErrNoSources = errors.New("dist: no usable source left")
	// ErrStopped: Want on a Downloader whose Run has returned.
	ErrStopped = errors.New("dist: downloader stopped")
)

// Downloader fills one sparse chunk file from a set of sources: a random
// walk over its absent chunks in the background, faults (Want) ahead of the
// walk, adjacent chunks glued into one request up to the source's cap.
//
// Presence is trusted: chunkfile journals each run before writing it and
// settles a crash's half-written chunks at Open, so a resumed download just
// walks what is absent.
type Downloader struct {
	f    *chunkfile.File
	hash Hash
	o    Options
	n, h int64

	mu      sync.Mutex
	cond    *sync.Cond
	srcs    []*srcState
	rr      int
	walkIn  *Bitmap // chunks in flight in walk runs
	faultIn *Bitmap // chunks in flight in fault runs
	faultQ  []int64
	queued  *Bitmap // chunks in faultQ
	nQueued int
	nFaults int           // fault runs in flight
	calm    chan struct{} // non-nil while faults are queued or in flight; closed when they end
	starved []int64       // faults no live source holds
	waiters map[int64]chan struct{}
	order   []uint32
	cursor  int
	gen     uint64 // bumps when chunks return or sources change
	passGen uint64
	err     error
	stopped bool
	whole   chan struct{}
	isWhole bool
	fatal   chan struct{}
	done    chan struct{} // Run returned

	start                   time.Time
	bytes, chunks, requests int64
	faultRequests           int64
	abandoned               int64 // walk chunks given back after a long pause
}

type srcState struct {
	s          Source
	have       *Bitmap // nil: every chunk (no announce)
	inflight   int
	bad, fails int
	benchUntil time.Time
	bench      time.Duration
	dropped    error
	lastDone   time.Time // last request that finished well
	lastErr    error
	benches    int
	requests   int64
	chunks     int64
}

func (s *srcState) holds(i int64) bool { return s.have == nil || s.have.Has(i) }

type job struct {
	src      *srcState
	first, n int64
	fault    bool
}

// New makes a downloader for f, the open chunk file named hash.
func New(f *chunkfile.File, hash Hash, srcs []Source, o Options) *Downloader {
	if o.Fetchers <= 0 {
		o.Fetchers = 8
	}
	if o.FaultWorkers <= 0 {
		o.FaultWorkers = 4
	}
	if o.MaxFailures <= 0 {
		o.MaxFailures = 3
	}
	if o.Refresh <= 0 {
		o.Refresh = 30 * time.Second
	}
	if o.WalkPause <= 0 {
		o.WalkPause = 5 * time.Second
	}
	if o.Seed == 0 {
		o.Seed = rand.Uint64() | 1
	}
	d := &Downloader{
		f: f, hash: hash, o: o, n: f.NumChunks(), h: f.HeaderChunks(),
		walkIn:  NewBitmap(f.NumChunks()),
		faultIn: NewBitmap(f.NumChunks()),
		queued:  NewBitmap(f.NumChunks()),
		waiters: map[int64]chan struct{}{},
		whole:   make(chan struct{}),
		fatal:   make(chan struct{}),
		done:    make(chan struct{}),
	}
	d.cond = sync.NewCond(&d.mu)
	for _, s := range srcs {
		d.srcs = append(d.srcs, &srcState{s: s})
	}
	if !o.OnDemand {
		d.order = make([]uint32, 0, d.n-d.h)
		for i := d.h; i < d.n; i++ {
			d.order = append(d.order, uint32(i))
		}
		r := rand.New(rand.NewPCG(o.Seed, o.Seed^0x9e3779b97f4a7c15))
		r.Shuffle(len(d.order), func(a, b int) { d.order[a], d.order[b] = d.order[b], d.order[a] })
	}
	return d
}

// Whole is closed once the walk finds every chunk present.
func (d *Downloader) Whole() <-chan struct{} { return d.whole }

// Run fetches until ctx ends (nil) or every source is dropped
// (ErrNoSources). The walk stops at whole; fault workers keep serving Want.
func (d *Downloader) Run(ctx context.Context) error {
	defer close(d.done)
	d.mu.Lock()
	d.start = time.Now()
	d.mu.Unlock()
	d.refresh(ctx)
	var wg sync.WaitGroup
	stop := context.AfterFunc(ctx, func() {
		d.mu.Lock()
		d.stopped = true
		d.cond.Broadcast()
		d.mu.Unlock()
	})
	defer stop()
	for w := range d.o.Fetchers + d.o.FaultWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.worker(ctx, w >= d.o.Fetchers)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(d.o.Refresh)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.fatal:
				return
			case <-t.C:
				d.refresh(ctx)
			}
		}
	}()
	wg.Wait()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

// refresh re-reads every live source's announce.
func (d *Downloader) refresh(ctx context.Context) {
	d.mu.Lock()
	live := make([]*srcState, 0, len(d.srcs))
	for _, s := range d.srcs {
		if s.dropped == nil {
			live = append(live, s)
		}
	}
	d.mu.Unlock()
	type res struct {
		s   *srcState
		b   *Bitmap
		err error
	}
	out := make(chan res, len(live))
	for _, s := range live {
		go func() {
			b, err := s.s.Have(ctx, d.hash, d.n)
			out <- res{s, b, err}
		}()
	}
	got := make([]res, 0, len(live))
	for range live {
		got = append(got, <-out)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range got {
		switch {
		case r.err == nil:
			r.s.have = r.b
		case errors.Is(r.err, ErrNoAnnounce):
			// Keep what Fetch answers taught us; a source that never
			// announced starts as holding everything.
		case errors.Is(r.err, ErrUnknown):
			d.drop(r.s, ErrUnknown)
		case ctx.Err() != nil:
		default:
			d.transportFailure(r.s, r.err)
		}
	}
	d.requeueStarved()
}

// requeueStarved puts the faults no source could serve back at the head of
// the queue: after an announce refresh or when a benched source returns.
func (d *Downloader) requeueStarved() {
	var back []int64
	for _, c := range d.starved {
		if !d.queued.Has(c) {
			back = append(back, c)
			d.setQueued(c)
		}
	}
	d.faultQ = append(back, d.faultQ...)
	d.starved = nil
	d.gen++
	d.cond.Broadcast()
}

func (d *Downloader) drop(s *srcState, why error) {
	if s.dropped != nil {
		return
	}
	s.dropped = why
	for _, o := range d.srcs {
		if o.dropped == nil {
			return
		}
	}
	d.err = ErrNoSources
	close(d.fatal)
}

func (d *Downloader) transportFailure(s *srcState, err error) {
	s.lastErr = err
	s.fails++
	if s.fails < d.o.MaxFailures {
		return
	}
	s.bench = min(max(2*s.bench, time.Second), time.Minute)
	s.benchUntil = time.Now().Add(s.bench)
	s.benches++
	s.fails = 0
	time.AfterFunc(s.bench, func() {
		d.mu.Lock()
		d.requeueStarved()
		d.mu.Unlock()
	})
}

// pick chooses the live, unbenched source holding chunk c with the fewest
// requests in flight. A walk run takes the next such source in rotation; a
// fault takes the one that last finished a request: its connections are
// hot, while a connection idle for longer than its retransmit timeout
// restarts TCP slow start and needs ~6 round trips for 1 MiB.
func (d *Downloader) pick(c int64, fault bool) *srcState {
	now := time.Now()
	var best *srcState
	d.rr++
	for k := range d.srcs {
		s := d.srcs[(k+d.rr)%len(d.srcs)]
		if s.dropped != nil || now.Before(s.benchUntil) || !s.holds(c) {
			continue
		}
		if best == nil || s.inflight < best.inflight || (fault && s.inflight == best.inflight && s.lastDone.After(best.lastDone)) {
			best = s
		}
	}
	return best
}

// walkable: absent and in no run. faultable: absent and in no fault run; a
// fault does not wait for a walk run that holds its chunk (on a shared link
// the walk's 16 MB runs stream at a fraction of the bandwidth each, and the
// chunk may be the run's last), it fetches the chunk on its own and the walk
// run skips it when it gets there.
func (d *Downloader) walkable(i int64) bool {
	return i >= d.h && i < d.n && !d.f.Present(i) && !d.walkIn.Has(i) && !d.faultIn.Has(i)
}

func (d *Downloader) faultable(i int64) bool {
	return i >= d.h && i < d.n && !d.f.Present(i) && !d.faultIn.Has(i)
}

func (d *Downloader) setQueued(i int64) {
	if !d.queued.Has(i) {
		d.queued.Set(i)
		d.nQueued++
		d.markBusy()
	}
}

func (d *Downloader) clearQueued(i int64) {
	if d.queued.Has(i) {
		d.queued.Clear(i)
		d.nQueued--
		d.maybeCalm()
	}
}

// Faults own the link: while any is queued or in flight, the walk starts
// no run and its in-flight runs stop reading (TCP flow control then stops
// their senders within about one round trip of data), so a fault's chunk
// does not queue behind walk bytes at a shared bottleneck.
func (d *Downloader) busy() bool { return d.nQueued > 0 || d.nFaults > 0 }

func (d *Downloader) markBusy() {
	if d.calm == nil {
		d.calm = make(chan struct{})
	}
}

func (d *Downloader) maybeCalm() {
	if d.calm != nil && !d.busy() {
		close(d.calm)
		d.calm = nil
		d.cond.Broadcast()
	}
}

var errPaused = errors.New("dist: walk run paused too long")

// gate is a walk run's body: Read waits while faults are busy, up to
// WalkPause in total, then fails with errPaused.
type gate struct {
	d      *Downloader
	ctx    context.Context
	r      interface{ Read([]byte) (int, error) }
	paused time.Duration
}

func (g *gate) Read(p []byte) (int, error) {
	g.d.mu.Lock()
	ch := g.d.calm
	g.d.mu.Unlock()
	if ch != nil {
		t0 := time.Now()
		t := time.NewTimer(g.d.o.WalkPause - g.paused)
		select {
		case <-ch:
		case <-t.C:
		case <-g.ctx.Done():
		}
		t.Stop()
		g.paused += time.Since(t0)
		if g.paused >= g.d.o.WalkPause {
			return 0, errPaused
		}
		if err := g.ctx.Err(); err != nil {
			return 0, err
		}
	}
	return g.r.Read(p)
}

func (d *Downloader) runBits(fault bool) *Bitmap {
	if fault {
		return d.faultIn
	}
	return d.walkIn
}

// glue grows [c, c+1) both ways over chunks ok accepts that s holds, up to
// s's cap, marks them in flight and returns the job.
func (d *Downloader) glue(s *srcState, c int64, fault bool, ok func(int64) bool) job {
	limit := s.s.MaxChunks()
	a, b := c, c+1
	for b-a < limit && ok(b) && s.holds(b) {
		b++
	}
	for b-a < limit && ok(a-1) && s.holds(a-1) {
		a--
	}
	in := d.runBits(fault)
	for i := a; i < b; i++ {
		in.Set(i)
		d.clearQueued(i)
	}
	s.inflight++
	if fault {
		d.nFaults++
		d.markBusy()
	}
	return job{src: s, first: a, n: b - a, fault: fault}
}

func (d *Downloader) takeFault() (job, bool) {
	for len(d.faultQ) > 0 {
		c := d.faultQ[0]
		d.faultQ = d.faultQ[1:]
		if !d.queued.Has(c) {
			continue // taken by a walk run already
		}
		if !d.faultable(c) {
			d.clearQueued(c)
			continue
		}
		s := d.pick(c, true)
		if s == nil {
			d.clearQueued(c)
			d.starved = append(d.starved, c)
			continue
		}
		return d.glue(s, c, true, func(i int64) bool { return d.queued.Has(i) && d.faultable(i) }), true
	}
	return job{}, false
}

func (d *Downloader) takeWalk() (job, bool) {
	for d.cursor < len(d.order) {
		c := int64(d.order[d.cursor])
		d.cursor++
		if d.f.Present(c) || d.walkIn.Has(c) || d.faultIn.Has(c) {
			continue
		}
		if s := d.pick(c, false); s != nil {
			return d.glue(s, c, false, d.walkable), true
		}
	}
	// End of a pass. Whole when nothing is absent; another pass when chunks
	// came back or sources changed since this one began.
	if d.isWhole {
		return job{}, false
	}
	missing := false
	for i := d.h; i < d.n; i++ {
		if !d.f.Present(i) {
			missing = true
			break
		}
	}
	if !missing {
		d.isWhole = true
		close(d.whole)
		return job{}, false
	}
	if d.gen != d.passGen {
		d.passGen = d.gen
		d.cursor = 0
		return d.takeWalk()
	}
	return job{}, false
}

func (d *Downloader) next(faultOnly bool) (job, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for {
		if d.stopped || d.err != nil {
			return job{}, false
		}
		if j, ok := d.takeFault(); ok {
			return j, true
		}
		if !faultOnly && d.order != nil && !d.busy() {
			if j, ok := d.takeWalk(); ok {
				return j, true
			}
		}
		d.cond.Wait()
	}
}

func (d *Downloader) worker(ctx context.Context, faultOnly bool) {
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	for {
		j, ok := d.next(faultOnly)
		if !ok {
			return
		}
		r, err := j.src.s.Fetch(ctx, d.hash, j.first, j.n)
		var got int64
		if err == nil {
			d.mu.Lock()
			d.requests++
			if j.fault {
				d.faultRequests++
			}
			j.src.requests++
			d.mu.Unlock()
			var body interface{ Read([]byte) (int, error) } = r
			if !j.fault {
				body = &gate{d: d, ctx: ctx, r: r}
			}
			got, err = WriteRun(d.f, body, j.first, j.n, *bp, func(i int64, wrote bool) { d.landed(j, i, wrote) })
			r.Close()
		}
		d.finish(ctx, j, got, err)
	}
}

// landed accounts chunk i of j and wakes its waiters.
func (d *Downloader) landed(j job, i int64, wrote bool) {
	s, e := d.f.Chunk(i)
	d.mu.Lock()
	d.runBits(j.fault).Clear(i)
	d.bytes += e - s
	if !wrote {
		d.mu.Unlock()
		return
	}
	d.chunks++
	j.src.chunks++
	if ch := d.waiters[i]; ch != nil {
		close(ch)
		delete(d.waiters, i)
	}
	d.mu.Unlock()
}

func (d *Downloader) finish(ctx context.Context, j job, got int64, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := j.src
	s.inflight--
	// Chunks that did not land go back; faulted ones to the head of the
	// fault queue.
	var back []int64
	in := d.runBits(j.fault)
	for i := j.first + got; i < j.first+j.n; i++ {
		in.Clear(i)
		if d.waiters[i] != nil && !d.queued.Has(i) {
			back = append(back, i)
			d.setQueued(i)
		}
	}
	if len(back) > 0 {
		d.faultQ = append(back, d.faultQ...)
	}
	if j.fault {
		d.nFaults--
		d.maybeCalm()
	}
	switch {
	case err == nil:
		s.fails = 0
		s.bench = 0
		s.lastDone = time.Now()
	case ctx.Err() != nil, errors.Is(err, errPaused):
		d.abandoned += j.n - got
	case errors.Is(err, chunkfile.ErrMismatch):
		s.bad++
		if s.bad >= d.o.MaxFailures {
			d.drop(s, fmt.Errorf("%d bad chunks", s.bad))
		}
	case errors.Is(err, ErrNotHeld):
		if s.have == nil {
			s.have = NewBitmap(d.n)
			for i := range d.n {
				s.have.Set(i)
			}
		}
		for i := j.first + got; i < j.first+j.n; i++ {
			s.have.Clear(i)
		}
	case errors.Is(err, ErrUnknown):
		d.drop(s, ErrUnknown)
	default:
		d.transportFailure(s, err)
	}
	if got < j.n {
		d.gen++
	}
	d.cond.Broadcast()
}

// Want returns once chunk i is present (fetching it ahead of the walk), or
// with ctx's error, or ErrNoSources. A reader that gets
// chunkfile.AbsentError{Chunk: i} calls Want(ctx, i) and reads again.
func (d *Downloader) Want(ctx context.Context, i int64) error {
	if i < 0 || i >= d.n {
		return fmt.Errorf("dist: chunk %d out of %d", i, d.n)
	}
	if d.f.Present(i) {
		return nil
	}
	d.mu.Lock()
	if d.f.Present(i) {
		d.mu.Unlock()
		return nil
	}
	if d.err != nil {
		d.mu.Unlock()
		return d.err
	}
	ch := d.waiters[i]
	if ch == nil {
		ch = make(chan struct{})
		d.waiters[i] = ch
	}
	if !d.faultIn.Has(i) && !d.queued.Has(i) {
		d.faultQ = append(d.faultQ, i)
		d.setQueued(i)
		d.cond.Broadcast()
	}
	d.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-d.fatal:
		return ErrNoSources
	case <-d.done:
		if d.f.Present(i) {
			return nil
		}
		return ErrStopped
	}
}

// Progress is a snapshot for the status line.
type Progress struct {
	Chunks, Present int64 // data chunks in the file, present now
	Bytes           int64 // received by this Downloader (skipped duplicates included)
	Fetched         int64 // chunks written
	Requests        int64 // Fetch calls that returned a body
	FaultRequests   int64 // of which for faults
	Abandoned       int64 // walk chunks given back after pausing WalkPause for faults
	Elapsed         time.Duration
	Rate            float64       // bytes/s since Run started
	ETA             time.Duration // absent bytes at Rate (0 if unknown)
	Sources         []SourceStatus
}

type SourceStatus struct {
	Name             string
	Requests, Chunks int64
	Inflight         int
	Bad              int
	Benched          bool
	Benches          int    // times benched
	LastErr          string // last transport error
	Dropped          string
}

// MeanRun is the mean glued run length in chunks.
func (p Progress) MeanRun() float64 {
	if p.Requests == 0 {
		return 0
	}
	return float64(p.Fetched) / float64(p.Requests)
}

func (d *Downloader) Progress() Progress {
	d.mu.Lock()
	defer d.mu.Unlock()
	p := Progress{Chunks: d.n - d.h, Bytes: d.bytes, Fetched: d.chunks, Requests: d.requests, FaultRequests: d.faultRequests, Abandoned: d.abandoned}
	var absent int64
	for i := d.h; i < d.n; i++ {
		if d.f.Present(i) {
			p.Present++
		} else {
			s, e := d.f.Chunk(i)
			absent += e - s
		}
	}
	if !d.start.IsZero() {
		p.Elapsed = time.Since(d.start)
		p.Rate = float64(d.bytes) / p.Elapsed.Seconds()
		if p.Rate > 0 {
			p.ETA = time.Duration(float64(absent) / p.Rate * float64(time.Second))
		}
	}
	now := time.Now()
	for _, s := range d.srcs {
		st := SourceStatus{Name: s.s.String(), Requests: s.requests, Chunks: s.chunks, Inflight: s.inflight, Bad: s.bad, Benched: now.Before(s.benchUntil)}
		if s.dropped != nil {
			st.Dropped = s.dropped.Error()
		}
		if s.lastErr != nil {
			st.LastErr = s.lastErr.Error()
		}
		st.Benches = s.benches
		p.Sources = append(p.Sources, st)
	}
	return p
}
