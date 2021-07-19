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
func (service *Admin) GetChainAliases(r *http.Request, args *GetChainAliasesArgs, reply *GetChainAliasesReply) error {
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

// SetLoggerLevelArgs defines the logger name and log&display levels to be set.
type SetLoggerLevelArgs struct {
	LogLevel     string `json:"logLevel"`
	DisplayLevel string `json:"displayLevel"`
	LoggerName   string `json:"loggerName"`
}

// SetLoggerLevel sets the log level and/or display level for loggers.
// If len([args.LoggerName]) == 0, sets the log/display level of all loggers.
// Otherwise, sets the log/display level of the loggers named in that argument.
// Sets the log level of these loggers to [args.LogLevel].
// If len([args.LogLevel]) == 0, doesn't set the log level of these loggers.
// If len([args.LogLevel]) > 0, must be a valid string representation of a log level.
// Sets the display level of these loggers to [args.LogLevel].
// If len([args.DisplayLevel]) == 0, doesn't set the display level of these loggers.
// If len([args.DisplayLevel]) > 0, must be a valid string representation of a log level.
func (service *Admin) SetLoggerLevel(r *http.Request, args *SetLoggerLevelArgs, reply *api.SuccessResponse) error {
	service.log.Info("Admin: SetLogLevels called with LoggerName: %q, LogLevel: %q, DisplayLevel: %q", args.LoggerName, args.LogLevel, args.DisplayLevel)
	if len(args.LogLevel) == 0 && len(args.DisplayLevel) == 0 {
		return errors.New("need to specify either displayLevel or logLevel")
	}

	var loggerNames []string
	// Empty name means all loggers
	if len(args.LoggerName) > 0 {
		loggerNames = []string{args.LoggerName}
	} else {
		loggerNames = service.logFactory.GetNames()
	}

	changeLogLevel := len(args.LogLevel) == 0
	var (
		changeLogLevelTo logging.Level
		err              error
	)
	if changeLogLevel {
		changeLogLevelTo, err = logging.ToLevel(args.LogLevel)
		if err != nil {
			return err
		}
	}
	changeDisplayLevel := len(args.LogLevel) == 0
	var changeDisplayLevelTo logging.Level
	if changeDisplayLevel {
		changeDisplayLevelTo, err = logging.ToLevel(args.LogLevel)
		if err != nil {
			return err
		}
	}

	for _, name := range loggerNames {
		if changeLogLevel {
			if err := service.logFactory.SetLogLevel(name, changeLogLevelTo); err != nil {
				return err
			}
		}
		if changeDisplayLevel {
			if err := service.logFactory.SetDisplayLevel(name, changeDisplayLevelTo); err != nil {
				return err
			}
		}
	}
	reply.Success = true
	return nil
}

type LoggerLevels struct {
	LogLevel     string `json:"logLevel"`
	DisplayLevel string `json:"displayLevel"`
}

// GetLoggerLevelArgs defines logger name arg.
type GetLoggerLevelArgs struct {
	LoggerName string `json:"loggerName"`
}

// GetLogLevelReply returns current log levels in a name=>level map.
type GetLoggerLevelReply struct {
	LoggerLevels map[string]LoggerLevels `json:"loggerLevels"`
}

// GetLogLevel get the log level and display level of loggers.
func (service *Admin) GetLoggerLevel(r *http.Request, args *GetLoggerLevelArgs, reply *GetLoggerLevelReply) error {
	service.log.Info("Admin: GetLoggerLevels called with LoggerName: %q", args.LoggerName)
	reply.LoggerLevels = make(map[string]LoggerLevels)
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
		reply.LoggerLevels[name] = LoggerLevels{
			LogLevel:     logLevel.String(),
			DisplayLevel: displayLevel.String(),
		}
	}
	return nil
}
