// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/api"
	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/logging"

	cjson "github.com/ava-labs/gecko/utils/json"
)

// Admin is the API service for node admin management
type Admin struct {
	log          logging.Logger
	performance  Performance
	chainManager chains.Manager
	httpServer   *api.Server
}

// NewService returns a new admin API service
func NewService(log logging.Logger, chainManager chains.Manager, httpServer *api.Server) *common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := cjson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	newServer.RegisterService(&Admin{
		log:          log,
		chainManager: chainManager,
		httpServer:   httpServer,
	}, "admin")
	return &common.HTTPHandler{Handler: newServer}
}

// StartCPUProfilerReply are the results from calling StartCPUProfiler
type StartCPUProfilerReply struct {
	Success bool `json:"success"`
}

// StartCPUProfiler starts a cpu profile writing to the specified file
func (service *Admin) StartCPUProfiler(_ *http.Request, _ *struct{}, reply *StartCPUProfilerReply) error {
	service.log.Info("Admin: StartCPUProfiler called")
	reply.Success = true
	return service.performance.StartCPUProfiler()
}

// StopCPUProfilerReply are the results from calling StopCPUProfiler
type StopCPUProfilerReply struct {
	Success bool `json:"success"`
}

// StopCPUProfiler stops the cpu profile
func (service *Admin) StopCPUProfiler(_ *http.Request, _ *struct{}, reply *StopCPUProfilerReply) error {
	service.log.Info("Admin: StopCPUProfiler called")

	reply.Success = true
	return service.performance.StopCPUProfiler()
}

// MemoryProfileReply are the results from calling MemoryProfile
type MemoryProfileReply struct {
	Success bool `json:"success"`
}

// MemoryProfile runs a memory profile writing to the specified file
func (service *Admin) MemoryProfile(_ *http.Request, _ *struct{}, reply *MemoryProfileReply) error {
	service.log.Info("Admin: MemoryProfile called")

	reply.Success = true
	return service.performance.MemoryProfile()
}

// LockProfileReply are the results from calling LockProfile
type LockProfileReply struct {
	Success bool `json:"success"`
}

// LockProfile runs a mutex profile writing to the specified file
func (service *Admin) LockProfile(_ *http.Request, _ *struct{}, reply *LockProfileReply) error {
	service.log.Info("Admin: LockProfile called")

	reply.Success = true
	return service.performance.LockProfile()
}

// AliasArgs are the arguments for calling Alias
type AliasArgs struct {
	Endpoint string `json:"endpoint"`
	Alias    string `json:"alias"`
}

// AliasReply are the results from calling Alias
type AliasReply struct {
	Success bool `json:"success"`
}

// Alias attempts to alias an HTTP endpoint to a new name
func (service *Admin) Alias(_ *http.Request, args *AliasArgs, reply *AliasReply) error {
	service.log.Info("Admin: Alias called with URL: %s, Alias: %s", args.Endpoint, args.Alias)

	reply.Success = true
	return service.httpServer.AddAliasesWithReadLock(args.Endpoint, args.Alias)
}

// AliasChainArgs are the arguments for calling AliasChain
type AliasChainArgs struct {
	Chain string `json:"chain"`
	Alias string `json:"alias"`
}

// AliasChainReply are the results from calling AliasChain
type AliasChainReply struct {
	Success bool `json:"success"`
}

// AliasChain attempts to alias a chain to a new name
func (service *Admin) AliasChain(_ *http.Request, args *AliasChainArgs, reply *AliasChainReply) error {
	service.log.Info("Admin: AliasChain called with Chain: %s, Alias: %s", args.Chain, args.Alias)

	chainID, err := service.chainManager.Lookup(args.Chain)
	if err != nil {
		return err
	}

	if err := service.chainManager.Alias(chainID, args.Alias); err != nil {
		return err
	}

	reply.Success = true
	return service.httpServer.AddAliasesWithReadLock("bc/"+chainID.String(), "bc/"+args.Alias)
}

// StacktraceReply are the results from calling Stacktrace
type StacktraceReply struct {
	Stacktrace string `json:"stacktrace"`
}

// Stacktrace returns the current global stacktrace
func (service *Admin) Stacktrace(_ *http.Request, _ *struct{}, reply *StacktraceReply) error {
	service.log.Info("Admin: Stacktrace called")

	reply.Stacktrace = logging.Stacktrace{Global: true}.String()
	return nil
}
