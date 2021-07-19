// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"errors"
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"

	cjson "github.com/ava-labs/avalanchego/utils/json"
)

const (
	maxAliasLength = 512

	// Name of file that stacktraces are written to
	stacktraceFile = "stacktrace.txt"
)

var errAliasTooLong = errors.New("alias length is too long")

// Admin is the API service for node admin management
type Admin struct {
	log          logging.Logger
	profiler     profiler.Profiler
	chainManager chains.Manager
	httpServer   *server.Server
	logFactory   logging.Factory
}

// NewService returns a new admin API service
func NewService(log logging.Logger, chainManager chains.Manager, httpServer *server.Server, profileDir string, logFactory logging.Factory) (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := cjson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := newServer.RegisterService(&Admin{
		log:          log,
		chainManager: chainManager,
		httpServer:   httpServer,
		profiler:     profiler.New(profileDir),
		logFactory:   logFactory,
	}, "admin"); err != nil {
		return nil, err
	}
	return &common.HTTPHandler{Handler: newServer}, nil
}

// StartCPUProfiler starts a cpu profile writing to the specified file
func (service *Admin) StartCPUProfiler(_ *http.Request, _ *struct{}, reply *api.SuccessResponse) error {
	service.log.Info("Admin: StartCPUProfiler called")
	reply.Success = true
	return service.profiler.StartCPUProfiler()
}

// StopCPUProfiler stops the cpu profile
func (service *Admin) StopCPUProfiler(_ *http.Request, _ *struct{}, reply *api.SuccessResponse) error {
	service.log.Info("Admin: StopCPUProfiler called")

	reply.Success = true
	return service.profiler.StopCPUProfiler()
}

// MemoryProfile runs a memory profile writing to the specified file
func (service *Admin) MemoryProfile(_ *http.Request, _ *struct{}, reply *api.SuccessResponse) error {
	service.log.Info("Admin: MemoryProfile called")

	reply.Success = true
	return service.profiler.MemoryProfile()
}

// LockProfile runs a mutex profile writing to the specified file
func (service *Admin) LockProfile(_ *http.Request, _ *struct{}, reply *api.SuccessResponse) error {
	service.log.Info("Admin: LockProfile called")

	reply.Success = true
	return service.profiler.LockProfile()
}

// AliasArgs are the arguments for calling Alias
type AliasArgs struct {
	Endpoint string `json:"endpoint"`
	Alias    string `json:"alias"`
}

// Alias attempts to alias an HTTP endpoint to a new name
func (service *Admin) Alias(_ *http.Request, args *AliasArgs, reply *api.SuccessResponse) error {
	service.log.Info("Admin: Alias called with URL: %s, Alias: %s", args.Endpoint, args.Alias)

	if len(args.Alias) > maxAliasLength {
		return errAliasTooLong
	}

	reply.Success = true
	return service.httpServer.AddAliasesWithReadLock(args.Endpoint, args.Alias)
}

// AliasChainArgs are the arguments for calling AliasChain
type AliasChainArgs struct {
	Chain string `json:"chain"`
	Alias string `json:"alias"`
}

// AliasChain attempts to alias a chain to a new name
func (service *Admin) AliasChain(_ *http.Request, args *AliasChainArgs, reply *api.SuccessResponse) error {
	service.log.Info("Admin: AliasChain called with Chain: %s, Alias: %s", args.Chain, args.Alias)

	if len(args.Alias) > maxAliasLength {
		return errAliasTooLong
	}
	chainID, err := service.chainManager.Lookup(args.Chain)
	if err != nil {
		return err
	}

	if err := service.chainManager.Alias(chainID, args.Alias); err != nil {
		return err
	}

	reply.Success = true
	return service.httpServer.AddAliasesWithReadLock(constants.ChainAliasPrefix+chainID.String(), constants.ChainAliasPrefix+args.Alias)
}

// GetChainAliasesArgs are the arguments for calling GetChainAliases
type GetChainAliasesArgs struct {
	Chain string `json:"chain"`
}

// GetChainAliasesReply are the aliases of the given chain
type GetChainAliasesReply struct {
	Aliases []string `json:"aliases"`
}

// GetChainAliases returns the aliases of the chain
func (service *Admin) GetChainAliases(_ *http.Request, args *GetChainAliasesArgs, reply *GetChainAliasesReply) error {
	service.log.Info("Admin: GetChainAliases called with Chain: %s", args.Chain)

	id, err := ids.FromString(args.Chain)
	if err != nil {
		return err
	}

	reply.Aliases = service.chainManager.Aliases(id)
	return nil
}

// Stacktrace returns the current global stacktrace
func (service *Admin) Stacktrace(_ *http.Request, _ *struct{}, reply *api.SuccessResponse) error {
	service.log.Info("Admin: Stacktrace called")

	reply.Success = true
	stacktrace := []byte(logging.Stacktrace{Global: true}.String())
	return perms.WriteFile(stacktraceFile, stacktrace, perms.ReadWrite)
}

// See SetLoggerLevel
type SetLoggerLevelArgs struct {
	LoggerName   string        `json:"loggerName"`
	LogLevel     logging.Level `json:"logLevel"`
	DisplayLevel logging.Level `json:"displayLevel"`
}

// SetLoggerLevel sets the log level and display level for loggers.
// If len([args.LoggerName]) == 0, sets the log/display level of all loggers.
// Sets the log level to [args.LogLevel] and the display level to [args.DisplayLevel].
// Both [args.LogLevel] and [args.DisplayLevel] must be provided.
func (service *Admin) SetLoggerLevel(_ *http.Request, args *SetLoggerLevelArgs, reply *api.SuccessResponse) error {
	service.log.Info("Admin: SetLogLevels called with LoggerName: %q, LogLevel: %q, DisplayLevel: %q", args.LoggerName, args.LogLevel, args.DisplayLevel)

	var loggerNames []string
	// Empty name means all loggers
	if len(args.LoggerName) > 0 {
		loggerNames = []string{args.LoggerName}
	} else {
		loggerNames = service.logFactory.GetNames()
	}

	for _, name := range loggerNames {
		if err := service.logFactory.SetLogLevel(name, args.LogLevel); err != nil {
			return err
		}
		if err := service.logFactory.SetDisplayLevel(name, args.DisplayLevel); err != nil {
			return err
		}
	}
	reply.Success = true
	return nil
}

type LogAndDisplayLevels struct {
	LogLevel     logging.Level `json:"logLevel"`
	DisplayLevel logging.Level `json:"displayLevel"`
}

// GetLoggerLevelArgs defines logger name arg.
type GetLoggerLevelArgs struct {
	LoggerName string `json:"loggerName"`
}

// GetLogLevelReply returns current log levels in a name=>level map.
type GetLoggerLevelReply struct {
	LoggerLevels map[string]LogAndDisplayLevels `json:"loggerLevels"`
}

// GetLogLevel get the log level and display level of loggers.
func (service *Admin) GetLoggerLevel(_ *http.Request, args *GetLoggerLevelArgs, reply *GetLoggerLevelReply) error {
	service.log.Info("Admin: GetLoggerLevels called with LoggerName: %q", args.LoggerName)
	reply.LoggerLevels = make(map[string]LogAndDisplayLevels)
	var loggerNames []string
	// Empty name means all loggers
	if len(args.LoggerName) > 0 {
		loggerNames = []string{args.LoggerName}
	} else {
		loggerNames = service.logFactory.GetNames()
	}

	for _, name := range loggerNames {
		logLevel, err := service.logFactory.GetLogLevel(name)
		if err != nil {
			return err
		}
		displayLevel, err := service.logFactory.GetDisplayLevel(name)
		if err != nil {
			return err
		}
		reply.LoggerLevels[name] = LogAndDisplayLevels{
			LogLevel:     logLevel,
			DisplayLevel: displayLevel,
		}
	}
	return nil
}
