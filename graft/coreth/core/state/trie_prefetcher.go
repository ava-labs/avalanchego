// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package state

import (
	"sync"
	"time"

	"github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

// triePrefetchMetricsPrefix is the prefix under which to publish the metrics.
const triePrefetchMetricsPrefix = "trie/prefetch/"

// triePrefetcher is an active prefetcher, which receives accounts or storage
// items and does trie-loading of them. The goal is to get as much useful content
// into the caches as possible.
//
// Note, the prefetcher's API is not thread safe.
type triePrefetcher struct {
	db       Database               // Database to fetch trie nodes through
	root     common.Hash            // Root hash of the account trie for metrics
	fetches  map[string]Trie        // Partially or fully fetcher tries
	fetchers map[string]*subfetcher // Subfetchers for each trie

	maxConcurrency int
	workers        *utils.BoundedWorkers

	subfetcherWorkersMeter metrics.Meter
	subfetcherWaitTimer    metrics.Counter
	subfetcherCopiesMeter  metrics.Meter

	accountLoadMeter  metrics.Meter
	accountDupMeter   metrics.Meter
	accountSkipMeter  metrics.Meter
	accountWasteMeter metrics.Meter

	storageFetchersMeter    metrics.Meter
	storageLoadMeter        metrics.Meter
	storageLargestLoadMeter metrics.Meter
	storageDupMeter         metrics.Meter
	storageSkipMeter        metrics.Meter
	storageWasteMeter       metrics.Meter
}

func newTriePrefetcher(db Database, root common.Hash, namespace string, maxConcurrency int) *triePrefetcher {
	prefix := triePrefetchMetricsPrefix + namespace
	return &triePrefetcher{
		db:       db,
		root:     root,
		fetchers: make(map[string]*subfetcher), // Active prefetchers use the fetchers map

		maxConcurrency: maxConcurrency,
		workers:        utils.NewBoundedWorkers(maxConcurrency), // Scale up as needed to [maxConcurrency]

		subfetcherWorkersMeter: metrics.GetOrRegisterMeter(prefix+"/subfetcher/workers", nil),
		subfetcherWaitTimer:    metrics.GetOrRegisterCounter(prefix+"/subfetcher/wait", nil),
		subfetcherCopiesMeter:  metrics.GetOrRegisterMeter(prefix+"/subfetcher/copies", nil),

		accountLoadMeter:  metrics.GetOrRegisterMeter(prefix+"/account/load", nil),
		accountDupMeter:   metrics.GetOrRegisterMeter(prefix+"/account/dup", nil),
		accountSkipMeter:  metrics.GetOrRegisterMeter(prefix+"/account/skip", nil),
		accountWasteMeter: metrics.GetOrRegisterMeter(prefix+"/account/waste", nil),

		storageFetchersMeter:    metrics.GetOrRegisterMeter(prefix+"/storage/fetchers", nil),
		storageLoadMeter:        metrics.GetOrRegisterMeter(prefix+"/storage/load", nil),
		storageLargestLoadMeter: metrics.GetOrRegisterMeter(prefix+"/storage/lload", nil),
		storageDupMeter:         metrics.GetOrRegisterMeter(prefix+"/storage/dup", nil),
		storageSkipMeter:        metrics.GetOrRegisterMeter(prefix+"/storage/skip", nil),
		storageWasteMeter:       metrics.GetOrRegisterMeter(prefix+"/storage/waste", nil),
	}
}

// close iterates over all the subfetchers, aborts any that were left spinning
// and reports the stats to the metrics subsystem.
func (p *triePrefetcher) close() {
	// If the prefetcher is an inactive one, bail out
	if p.fetches != nil {
		return
	}

	// Collect stats from all fetchers
	var (
		storageFetchers int64
		largestLoad     int64
	)
	for _, fetcher := range p.fetchers {
		fetcher.abort() // safe to call multiple times (should be a no-op on happy path)

		if metrics.Enabled {
			p.subfetcherCopiesMeter.Mark(int64(fetcher.copies()))

			if fetcher.root == p.root {
				p.accountLoadMeter.Mark(int64(len(fetcher.seen)))
				p.accountDupMeter.Mark(int64(fetcher.dups))
				p.accountSkipMeter.Mark(int64(fetcher.skips()))

				for _, key := range fetcher.used {
					delete(fetcher.seen, string(key))
				}
				p.accountWasteMeter.Mark(int64(len(fetcher.seen)))
			} else {
				storageFetchers++
				oseen := int64(len(fetcher.seen))
				if oseen > largestLoad {
					largestLoad = oseen
				}
				p.storageLoadMeter.Mark(oseen)
				p.storageDupMeter.Mark(int64(fetcher.dups))
				p.storageSkipMeter.Mark(int64(fetcher.skips()))

				for _, key := range fetcher.used {
					delete(fetcher.seen, string(key))
				}
				p.storageWasteMeter.Mark(int64(len(fetcher.seen)))
			}
		}
	}
	if metrics.Enabled {
		p.storageFetchersMeter.Mark(storageFetchers)
		p.storageLargestLoadMeter.Mark(largestLoad)
	}

	// Stop all workers once fetchers are aborted (otherwise
	// could stop while waiting)
	//
	// Record number of workers that were spawned during this run
	workersUsed := int64(p.workers.Wait())
	if metrics.Enabled {
		p.subfetcherWorkersMeter.Mark(workersUsed)
	}

	// Clear out all fetchers (will crash on a second call, deliberate)
	p.fetchers = nil
}

// copy creates a deep-but-inactive copy of the trie prefetcher. Any trie data
// already loaded will be copied over, but no goroutines will be started. This
// is mostly used in the miner which creates a copy of it's actively mutated
// state to be sealed while it may further mutate the state.
func (p *triePrefetcher) copy() *triePrefetcher {
	copy := &triePrefetcher{
		db:      p.db,
		root:    p.root,
		fetches: make(map[string]Trie), // Active prefetchers use the fetchers map

		subfetcherWorkersMeter: p.subfetcherWorkersMeter,
		subfetcherWaitTimer:    p.subfetcherWaitTimer,
		subfetcherCopiesMeter:  p.subfetcherCopiesMeter,

		accountLoadMeter:  p.accountLoadMeter,
		accountDupMeter:   p.accountDupMeter,
		accountSkipMeter:  p.accountSkipMeter,
		accountWasteMeter: p.accountWasteMeter,

		storageFetchersMeter:    p.storageFetchersMeter,
		storageLoadMeter:        p.storageLoadMeter,
		storageLargestLoadMeter: p.storageLargestLoadMeter,
		storageDupMeter:         p.storageDupMeter,
		storageSkipMeter:        p.storageSkipMeter,
		storageWasteMeter:       p.storageWasteMeter,
	}
	// If the prefetcher is already a copy, duplicate the data
	if p.fetches != nil {
		for root, fetch := range p.fetches {
			if fetch == nil {
				continue
			}
			copy.fetches[root] = p.db.CopyTrie(fetch)
		}
		return copy
	}
	// Otherwise we're copying an active fetcher, retrieve the current states
	for id, fetcher := range p.fetchers {
		copy.fetches[id] = fetcher.peek()
	}
	return copy
}

// prefetch schedules a batch of trie items to prefetch.
func (p *triePrefetcher) prefetch(owner common.Hash, root common.Hash, addr common.Address, keys [][]byte) {
	// If the prefetcher is an inactive one, bail out
	if p.fetches != nil {
		return
	}

	// Active fetcher, schedule the retrievals
	id := p.trieID(owner, root)
	fetcher := p.fetchers[id]
	if fetcher == nil {
		fetcher = newSubfetcher(p, owner, root, addr)
		p.fetchers[id] = fetcher
	}
	fetcher.schedule(keys)
}

// trie returns the trie matching the root hash, or nil if the prefetcher doesn't
// have it.
func (p *triePrefetcher) trie(owner common.Hash, root common.Hash) Trie {
	// If the prefetcher is inactive, return from existing deep copies
	id := p.trieID(owner, root)
	if p.fetches != nil {
		trie := p.fetches[id]
		if trie == nil {
			return nil
		}
		return p.db.CopyTrie(trie)
	}

	// Otherwise the prefetcher is active, bail if no trie was prefetched for this root
	fetcher := p.fetchers[id]
	if fetcher == nil {
		return nil
	}

	// Wait for the fetcher to finish and shutdown orchestrator, if it exists
	start := time.Now()
	fetcher.wait()
	if metrics.Enabled {
		p.subfetcherWaitTimer.Inc(time.Since(start).Milliseconds())
	}

	// Return a copy of one of the prefetched tries
	trie := fetcher.peek()
	if trie == nil {
		return nil
	}
	return trie
}

// used marks a batch of state items used to allow creating statistics as to
// how useful or wasteful the prefetcher is.
func (p *triePrefetcher) used(owner common.Hash, root common.Hash, used [][]byte) {
	if fetcher := p.fetchers[p.trieID(owner, root)]; fetcher != nil {
		fetcher.used = used
	}
}

// trieID returns an unique trie identifier consists the trie owner and root hash.
func (p *triePrefetcher) trieID(owner common.Hash, root common.Hash) string {
	return string(append(owner.Bytes(), root.Bytes()...))
}

// subfetcher is a trie fetcher goroutine responsible for pulling entries for a
// single trie. It is spawned when a new root is encountered and lives until the
// main prefetcher is paused and either all requested items are processed or if
// the trie being worked on is retrieved from the prefetcher.
type subfetcher struct {
	p *triePrefetcher

	db    Database       // Database to load trie nodes through
	state common.Hash    // Root hash of the state to prefetch
	owner common.Hash    // Owner of the trie, usually account hash
	root  common.Hash    // Root hash of the trie to prefetch
	addr  common.Address // Address of the account that the trie belongs to

	to *trieOrchestrator // Orchestrate concurrent fetching of a single trie

	seen map[string]struct{} // Tracks the entries already loaded
	dups int                 // Number of duplicate preload tasks
	used [][]byte            // Tracks the entries used in the end
}

// newSubfetcher creates a goroutine to prefetch state items belonging to a
// particular root hash.
func newSubfetcher(p *triePrefetcher, owner common.Hash, root common.Hash, addr common.Address) *subfetcher {
	sf := &subfetcher{
		p:     p,
		db:    p.db,
		state: p.root,
		owner: owner,
		root:  root,
		addr:  addr,
		seen:  make(map[string]struct{}),
	}
	sf.to = newTrieOrchestrator(sf)
	if sf.to != nil {
		go sf.to.processTasks()
	}
	// We return [sf] here to ensure we don't try to re-create if
	// we aren't able to setup a [newTrieOrchestrator] the first time.
	return sf
}

// schedule adds a batch of trie keys to the queue to prefetch.
// This should never block, so an array is used instead of a channel.
//
// This is not thread-safe.
func (sf *subfetcher) schedule(keys [][]byte) {
	// Append the tasks to the current queue
	tasks := make([][]byte, 0, len(keys))
	for _, key := range keys {
		// Check if keys already seen
		sk := string(key)
		if _, ok := sf.seen[sk]; ok {
			sf.dups++
			continue
		}
		sf.seen[sk] = struct{}{}
		tasks = append(tasks, key)
	}

	// After counting keys, exit if they can't be prefetched
	if sf.to == nil {
		return
	}

	// Add tasks to queue for prefetching
	sf.to.enqueueTasks(tasks)
}

// peek tries to retrieve a deep copy of the fetcher's trie in whatever form it
// is currently.
func (sf *subfetcher) peek() Trie {
	if sf.to == nil {
		return nil
	}
	return sf.to.copyBase()
}

// wait must only be called if [triePrefetcher] has not been closed. If this happens,
// workers will not finish.
func (sf *subfetcher) wait() {
	if sf.to == nil {
		// Unable to open trie
		return
	}
	sf.to.wait()
}

func (sf *subfetcher) abort() {
	if sf.to == nil {
		// Unable to open trie
		return
	}
	sf.to.abort()
}

func (sf *subfetcher) skips() int {
	if sf.to == nil {
		// Unable to open trie
		return 0
	}
	return sf.to.skipCount()
}

func (sf *subfetcher) copies() int {
	if sf.to == nil {
		// Unable to open trie
		return 0
	}
	return sf.to.copies
}

// trieOrchestrator is not thread-safe.
type trieOrchestrator struct {
	sf *subfetcher

	// base is an unmodified Trie we keep for
	// creating copies for each worker goroutine.
	//
	// We care more about quick copies than good copies
	// because most (if not all) of the nodes that will be populated
	// in the copy will come from the underlying triedb cache. Ones
	// that don't come from this cache probably had to be fetched
	// from disk anyways.
	base     Trie
	baseLock sync.Mutex

	tasksAllowed bool
	skips        int // number of tasks skipped
	pendingTasks [][]byte
	taskLock     sync.Mutex

	processingTasks sync.WaitGroup

	wake     chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
	loopTerm chan struct{}

	copies      int
	copyChan    chan Trie
	copySpawner chan struct{}
}

func newTrieOrchestrator(sf *subfetcher) *trieOrchestrator {
	// Start by opening the trie and stop processing if it fails
	var (
		base Trie
		err  error
	)
	if sf.owner == (common.Hash{}) {
		base, err = sf.db.OpenTrie(sf.root)
		if err != nil {
			log.Warn("Trie prefetcher failed opening trie", "root", sf.root, "err", err)
			return nil
		}
	} else {
		base, err = sf.db.OpenStorageTrie(sf.state, sf.owner, sf.root)
		if err != nil {
			log.Warn("Trie prefetcher failed opening trie", "root", sf.root, "err", err)
			return nil
		}
	}

	// Instantiate trieOrchestrator
	to := &trieOrchestrator{
		sf:   sf,
		base: base,

		tasksAllowed: true,
		wake:         make(chan struct{}, 1),
		stop:         make(chan struct{}),
		loopTerm:     make(chan struct{}),

		copyChan:    make(chan Trie, sf.p.maxConcurrency),
		copySpawner: make(chan struct{}, sf.p.maxConcurrency),
	}

	// Create initial trie copy
	to.copies++
	to.copySpawner <- struct{}{}
	to.copyChan <- to.copyBase()
	return to
}

func (to *trieOrchestrator) copyBase() Trie {
	to.baseLock.Lock()
	defer to.baseLock.Unlock()

	return to.sf.db.CopyTrie(to.base)
}

func (to *trieOrchestrator) skipCount() int {
	to.taskLock.Lock()
	defer to.taskLock.Unlock()

	return to.skips
}

func (to *trieOrchestrator) enqueueTasks(tasks [][]byte) {
	to.taskLock.Lock()
	defer to.taskLock.Unlock()

	if len(tasks) == 0 {
		return
	}

	// Add tasks to [pendingTasks]
	if !to.tasksAllowed {
		to.skips += len(tasks)
		return
	}
	to.processingTasks.Add(len(tasks))
	to.pendingTasks = append(to.pendingTasks, tasks...)

	// Wake up processor
	select {
	case to.wake <- struct{}{}:
	default:
	}
}

func (to *trieOrchestrator) handleStop(remaining int) {
	to.taskLock.Lock()
	to.skips += remaining
	to.taskLock.Unlock()
	to.processingTasks.Add(-remaining)
}

func (to *trieOrchestrator) processTasks() {
	defer close(to.loopTerm)

	for {
		// Determine if we should process or exit
		select {
		case <-to.wake:
		case <-to.stop:
			return
		}

		// Get current tasks
		to.taskLock.Lock()
		tasks := to.pendingTasks
		to.pendingTasks = nil
		to.taskLock.Unlock()

		// Enqueue more work as soon as trie copies are available
		lt := len(tasks)
		for i := 0; i < lt; i++ {
			// Try to stop as soon as possible, if channel is closed
			remaining := lt - i
			select {
			case <-to.stop:
				to.handleStop(remaining)
				return
			default:
			}

			// Try to create to get an active copy first (select is non-deterministic,
			// so we may end up creating a new copy when we don't need to)
			var t Trie
			select {
			case t = <-to.copyChan:
			default:
				// Wait for an available copy or create one, if we weren't
				// able to get a previously created copy
				select {
				case <-to.stop:
					to.handleStop(remaining)
					return
				case t = <-to.copyChan:
				case to.copySpawner <- struct{}{}:
					to.copies++
					t = to.copyBase()
				}
			}

			// Enqueue work, unless stopped.
			fTask := tasks[i]
			f := func() {
				// Perform task
				var err error
				if len(fTask) == common.AddressLength {
					_, err = t.GetAccount(common.BytesToAddress(fTask))
				} else {
					_, err = t.GetStorage(to.sf.addr, fTask)
				}
				if err != nil {
					log.Error("Trie prefetcher failed fetching", "root", to.sf.root, "err", err)
				}
				to.processingTasks.Done()

				// Return copy when we are done with it, so someone else can use it
				//
				// channel is buffered and will not block
				to.copyChan <- t
			}

			// Enqueue task for processing (may spawn new goroutine
			// if not at [maxConcurrency])
			//
			// If workers are stopped before calling [Execute], this function may
			// panic.
			to.sf.p.workers.Execute(f)
		}
	}
}

func (to *trieOrchestrator) stopAcceptingTasks() {
	to.taskLock.Lock()
	defer to.taskLock.Unlock()

	if !to.tasksAllowed {
		return
	}
	to.tasksAllowed = false

	// We don't clear [to.pendingTasks] here because
	// it will be faster to prefetch them even though we
	// are still waiting.
}

// wait stops accepting new tasks and waits for ongoing tasks to complete. If
// wait is called, it is not necessary to call [abort].
//
// It is safe to call wait multiple times.
func (to *trieOrchestrator) wait() {
	// Prevent more tasks from being enqueued
	to.stopAcceptingTasks()

	// Wait for processing tasks to complete
	to.processingTasks.Wait()

	// Stop orchestrator loop
	to.stopOnce.Do(func() {
		close(to.stop)
	})
	<-to.loopTerm
}

// abort stops any ongoing tasks and shuts down the orchestrator loop. If abort
// is called, it is not necessary to call [wait].
//
// It is safe to call abort multiple times.
func (to *trieOrchestrator) abort() {
	// Prevent more tasks from being enqueued
	to.stopAcceptingTasks()

	// Stop orchestrator loop
	to.stopOnce.Do(func() {
		close(to.stop)
	})
	<-to.loopTerm

	// Capture any dangling pending tasks (processTasks
	// may exit before enqueing all pendingTasks)
	to.taskLock.Lock()
	pendingCount := len(to.pendingTasks)
	to.skips += pendingCount
	to.pendingTasks = nil
	to.taskLock.Unlock()
	to.processingTasks.Add(-pendingCount)

	// Wait for processing tasks to complete
	to.processingTasks.Wait()
}
