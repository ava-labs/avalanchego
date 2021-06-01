// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package profiler

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sync/errgroup"
)

type ContinuousProfiler interface {
	Dispatch() error
	Shutdown()
}

type continuousProfiler struct {
	profiler *profiler
	dur      time.Duration
	maxIndex int
	closer   chan struct{}
}

func NewContinuous(dir string, dur time.Duration, maxIndex int) ContinuousProfiler {
	return &continuousProfiler{
		profiler: new(dir),
		dur:      dur,
		maxIndex: maxIndex,
		closer:   make(chan struct{}),
	}
}

func (p *continuousProfiler) Dispatch() error {
	t := time.NewTimer(p.dur)
	defer t.Stop()

	for {
		if err := p.start(); err != nil {
			return err
		}

		select {
		case <-p.closer:
			return p.stop()
		case <-t.C:
			if err := p.stop(); err != nil {
				return err
			}
		}

		if err := p.rotate(); err != nil {
			return err
		}

		t.Reset(p.dur)
	}
}

func (p *continuousProfiler) start() error {
	return p.profiler.StartCPUProfiler()
}

func (p *continuousProfiler) stop() error {
	g := errgroup.Group{}
	g.Go(p.profiler.StopCPUProfiler)
	g.Go(p.profiler.MemoryProfile)
	g.Go(p.profiler.LockProfile)
	return g.Wait()
}

func (p *continuousProfiler) rotate() error {
	g := errgroup.Group{}
	g.Go(func() error { return rotate(p.profiler.cpuProfileName, p.maxIndex) })
	g.Go(func() error { return rotate(p.profiler.memProfileName, p.maxIndex) })
	g.Go(func() error { return rotate(p.profiler.lockProfileName, p.maxIndex) })
	return g.Wait()
}

func (p *continuousProfiler) Shutdown() {
	close(p.closer)
}

func rotate(name string, maxIndex int) error {
	for i := maxIndex - 1; i > 0; i-- {
		sourceFilename := fmt.Sprintf("%s.%d", name, i)
		_, err := os.Stat(sourceFilename)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}

		destFilename := fmt.Sprintf("%s.%d", name, i+1)
		if err := os.Rename(sourceFilename, destFilename); err != nil {
			return err
		}
	}
	destFilename := fmt.Sprintf("%s.1", name)
	return os.Rename(name, destFilename)
}
