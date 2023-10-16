// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ethereum/go-ethereum/log"
)

// Admin is the API service for admin API calls
type Admin struct {
	vm       *VM
	profiler profiler.Profiler
}

func NewAdminService(vm *VM, performanceDir string) *Admin {
	return &Admin{
		vm:       vm,
		profiler: profiler.New(performanceDir),
	}
}

// StartCPUProfiler starts a cpu profile writing to the specified file
func (p *Admin) StartCPUProfiler(_ *http.Request, _ *struct{}, _ *api.EmptyReply) error {
	log.Info("Admin: StartCPUProfiler called")

	p.vm.ctx.Lock.Lock()
	defer p.vm.ctx.Lock.Unlock()

	return p.profiler.StartCPUProfiler()
}

// StopCPUProfiler stops the cpu profile
func (p *Admin) StopCPUProfiler(r *http.Request, _ *struct{}, _ *api.EmptyReply) error {
	log.Info("Admin: StopCPUProfiler called")

	p.vm.ctx.Lock.Lock()
	defer p.vm.ctx.Lock.Unlock()

	return p.profiler.StopCPUProfiler()
}

// MemoryProfile runs a memory profile writing to the specified file
func (p *Admin) MemoryProfile(_ *http.Request, _ *struct{}, _ *api.EmptyReply) error {
	log.Info("Admin: MemoryProfile called")

	p.vm.ctx.Lock.Lock()
	defer p.vm.ctx.Lock.Unlock()

	return p.profiler.MemoryProfile()
}

// LockProfile runs a mutex profile writing to the specified file
func (p *Admin) LockProfile(_ *http.Request, _ *struct{}, _ *api.EmptyReply) error {
	log.Info("Admin: LockProfile called")

	p.vm.ctx.Lock.Lock()
	defer p.vm.ctx.Lock.Unlock()

	return p.profiler.LockProfile()
}

type SetLogLevelArgs struct {
	Level string `json:"level"`
}

func (p *Admin) SetLogLevel(_ *http.Request, args *SetLogLevelArgs, reply *api.EmptyReply) error {
	log.Info("EVM: SetLogLevel called", "logLevel", args.Level)

	p.vm.ctx.Lock.Lock()
	defer p.vm.ctx.Lock.Unlock()

	if err := p.vm.logger.SetLogLevel(args.Level); err != nil {
		return fmt.Errorf("failed to parse log level: %w ", err)
	}
	return nil
}

type ConfigReply struct {
	Config *Config `json:"config"`
}

func (p *Admin) GetVMConfig(_ *http.Request, _ *struct{}, reply *ConfigReply) error {
	reply.Config = &p.vm.config
	return nil
}
