// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package profiler

import (
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/filesystem"
)

// Config that is used to describe the options of the continuous profiler.
type Config struct {
	Dir         string        `json:"dir"`
	Enabled     bool          `json:"enabled"`
	Freq        time.Duration `json:"freq"`
	MaxNumFiles int           `json:"maxNumFiles"`
}

// ContinuousProfiler periodically captures CPU, memory, and lock profiles
type ContinuousProfiler interface {
	Dispatch() error
	Shutdown()
}

type continuousProfiler struct {
	profiler    *profiler
	freq        time.Duration
	maxNumFiles int

	// Dispatch returns when closer is closed
	closer chan struct{}
}

func NewContinuous(dir string, freq time.Duration, maxNumFiles int) ContinuousProfiler {
	return &continuousProfiler{
		profiler:    newProfiler(dir),
		freq:        freq,
		maxNumFiles: maxNumFiles,
		closer:      make(chan struct{}),
	}
}

func (p *continuousProfiler) Dispatch() error {
	t := time.NewTicker(p.freq)
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
	g.Go(func() error {
		return rotate(p.profiler.cpuProfileName, p.maxNumFiles)
	})
	g.Go(func() error {
		return rotate(p.profiler.memProfileName, p.maxNumFiles)
	})
	g.Go(func() error {
		return rotate(p.profiler.lockProfileName, p.maxNumFiles)
	})
	return g.Wait()
}

func (p *continuousProfiler) Shutdown() {
	close(p.closer)
}

// Renames the file at [name] to [name].1, the file at [name].1 to [name].2, etc.
// Assumes that there is a file at [name].
func rotate(name string, maxNumFiles int) error {
	for i := maxNumFiles - 1; i > 0; i-- {
		sourceFilename := fmt.Sprintf("%s.%d", name, i)
		destFilename := fmt.Sprintf("%s.%d", name, i+1)
		if _, err := filesystem.RenameIfExists(sourceFilename, destFilename); err != nil {
			return err
		}
	}
	destFilename := name + ".1"
	_, err := filesystem.RenameIfExists(name, destFilename)
	return err
}
