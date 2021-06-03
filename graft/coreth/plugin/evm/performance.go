// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"

	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ethereum/go-ethereum/log"
)

// Performance is the API service for coreth performance
type Performance struct {
	profiler profiler.Profiler
}

func NewPerformanceService(dir string) *Performance {
	return &Performance{
		profiler: profiler.New(dir),
	}
}

// StartCPUProfiler starts a cpu profile writing to the specified file
func (p *Performance) StartCPUProfiler(ctx context.Context) (bool, error) {
	log.Info("Admin: StartCPUProfiler called")

	err := p.profiler.StartCPUProfiler()
	return err == nil, err
}

// StopCPUProfiler stops the cpu profile
func (p *Performance) StopCPUProfiler(ctx context.Context) (bool, error) {
	log.Info("Admin: StopCPUProfiler called")

	err := p.profiler.StopCPUProfiler()
	return err == nil, err
}

// MemoryProfile runs a memory profile writing to the specified file
func (p *Performance) MemoryProfile(ctx context.Context) (bool, error) {
	log.Info("Admin: MemoryProfile called")

	err := p.profiler.MemoryProfile()
	return err == nil, err
}

// LockProfile runs a mutex profile writing to the specified file
func (p *Performance) LockProfile(ctx context.Context) (bool, error) {
	log.Info("Admin: LockProfile called")

	err := p.profiler.LockProfile()
	return err == nil, err
}
