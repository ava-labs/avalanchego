// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"net/http"
	"path"

	"github.com/gorilla/rpc/v2"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/registry"
)

const (
	maxAliasLength = 512

	// Name of file that stacktraces are written to
	stacktraceFile = "stacktrace.txt"
)

var (
	errAliasTooLong = errors.New("alias length is too long")
	errNoLogLevel   = errors.New("need to specify either displayLevel or logLevel")
)

type Config struct {
	Secret       string
	Log          logging.Logger
	ProfileDir   string
	LogFactory   logging.Factory
	NodeConfig   interface{}
	ChainManager chains.Manager
	HTTPServer   server.PathAdderWithReadLock
	VMRegistry   registry.VMRegistry
	VMManager    vms.Manager

	StakingTLSCert tls.Certificate
}

// Admin is the API service for node admin management
type Admin struct {
	Config
	profiler profiler.Profiler
}

type Secret struct {
	Secret string `json:"secret"`
}

func (s *Secret) GetSecret() string {
	return s.Secret
}

type ISecret interface {
	GetSecret() string
}

// NewService returns a new admin API service.
// All of the fields in [config] must be set.
func NewService(config Config) (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	admin := &Admin{
		Config:   config,
		profiler: profiler.New(config.ProfileDir),
	}
	if err := newServer.RegisterService(admin, "admin"); err != nil {
		return nil, err
	}
	newServer.RegisterValidateRequestFunc(admin.ValidateRequest)
	return &common.HTTPHandler{Handler: newServer}, nil
}

func (a *Admin) ValidateRequest(_ *rpc.RequestInfo, i interface{}) error {
	if secret, ok := i.(ISecret); !ok || secret.GetSecret() != a.Secret {
		return errors.New("secret arg missing or wrong")
	}
	return nil
}

// StartCPUProfiler starts a cpu profile writing to the specified file
func (a *Admin) StartCPUProfiler(_ *http.Request, args *Secret, _ *api.EmptyReply) error { //nolint:revive
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "startCPUProfiler"),
	)

	return a.profiler.StartCPUProfiler()
}

// StopCPUProfiler stops the cpu profile
func (a *Admin) StopCPUProfiler(_ *http.Request, args *Secret, _ *api.EmptyReply) error { //nolint:revive
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "stopCPUProfiler"),
	)

	return a.profiler.StopCPUProfiler()
}

// MemoryProfile runs a memory profile writing to the specified file
func (a *Admin) MemoryProfile(_ *http.Request, args *Secret, _ *api.EmptyReply) error { //nolint:revive
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "memoryProfile"),
	)

	return a.profiler.MemoryProfile()
}

// LockProfile runs a mutex profile writing to the specified file
func (a *Admin) LockProfile(_ *http.Request, args *Secret, _ *api.EmptyReply) error { //nolint:revive
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "lockProfile"),
	)

	return a.profiler.LockProfile()
}

// AliasArgs are the arguments for calling Alias
type AliasArgs struct {
	Secret
	Endpoint string `json:"endpoint"`
	Alias    string `json:"alias"`
}

// Alias attempts to alias an HTTP endpoint to a new name
func (a *Admin) Alias(_ *http.Request, args *AliasArgs, _ *api.EmptyReply) error {
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "alias"),
		logging.UserString("endpoint", args.Endpoint),
		logging.UserString("alias", args.Alias),
	)

	if len(args.Alias) > maxAliasLength {
		return errAliasTooLong
	}

	return a.HTTPServer.AddAliasesWithReadLock(args.Endpoint, args.Alias)
}

// AliasChainArgs are the arguments for calling AliasChain
type AliasChainArgs struct {
	Secret
	Chain string `json:"chain"`
	Alias string `json:"alias"`
}

// AliasChain attempts to alias a chain to a new name
func (a *Admin) AliasChain(_ *http.Request, args *AliasChainArgs, _ *api.EmptyReply) error {
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "aliasChain"),
		logging.UserString("chain", args.Chain),
		logging.UserString("alias", args.Alias),
	)

	if len(args.Alias) > maxAliasLength {
		return errAliasTooLong
	}
	chainID, err := a.ChainManager.Lookup(args.Chain)
	if err != nil {
		return err
	}

	if err := a.ChainManager.Alias(chainID, args.Alias); err != nil {
		return err
	}

	endpoint := path.Join(constants.ChainAliasPrefix, chainID.String())
	alias := path.Join(constants.ChainAliasPrefix, args.Alias)
	return a.HTTPServer.AddAliasesWithReadLock(endpoint, alias)
}

// GetChainAliasesArgs are the arguments for calling GetChainAliases
type GetChainAliasesArgs struct {
	Secret
	Chain string `json:"chain"`
}

// GetChainAliasesReply are the aliases of the given chain
type GetChainAliasesReply struct {
	Aliases []string `json:"aliases"`
}

// GetChainAliases returns the aliases of the chain
func (a *Admin) GetChainAliases(_ *http.Request, args *GetChainAliasesArgs, reply *GetChainAliasesReply) error {
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "getChainAliases"),
		logging.UserString("chain", args.Chain),
	)

	id, err := ids.FromString(args.Chain)
	if err != nil {
		return err
	}

	reply.Aliases, err = a.ChainManager.Aliases(id)
	return err
}

// Stacktrace returns the current global stacktrace
func (a *Admin) Stacktrace(_ *http.Request, args *Secret, _ *api.EmptyReply) error { //nolint:revive
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "stacktrace"),
	)

	stacktrace := []byte(utils.GetStacktrace(true))
	return perms.WriteFile(stacktraceFile, stacktrace, perms.ReadWrite)
}

// See SetLoggerLevel
type SetLoggerLevelArgs struct {
	Secret
	LoggerName   string         `json:"loggerName"`
	LogLevel     *logging.Level `json:"logLevel"`
	DisplayLevel *logging.Level `json:"displayLevel"`
}

// SetLoggerLevel sets the log level and/or display level for loggers.
// If len([args.LoggerName]) == 0, sets the log/display level of all loggers.
// Otherwise, sets the log/display level of the loggers named in that argument.
// Sets the log level of these loggers to args.LogLevel.
// If args.LogLevel == nil, doesn't set the log level of these loggers.
// If args.LogLevel != nil, must be a valid string representation of a log level.
// Sets the display level of these loggers to args.LogLevel.
// If args.DisplayLevel == nil, doesn't set the display level of these loggers.
// If args.DisplayLevel != nil, must be a valid string representation of a log level.
func (a *Admin) SetLoggerLevel(_ *http.Request, args *SetLoggerLevelArgs, _ *api.EmptyReply) error {
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "setLoggerLevel"),
		logging.UserString("loggerName", args.LoggerName),
		zap.Stringer("logLevel", args.LogLevel),
		zap.Stringer("displayLevel", args.DisplayLevel),
	)

	if args.LogLevel == nil && args.DisplayLevel == nil {
		return errNoLogLevel
	}

	var loggerNames []string
	if len(args.LoggerName) > 0 {
		loggerNames = []string{args.LoggerName}
	} else {
		// Empty name means all loggers
		loggerNames = a.LogFactory.GetLoggerNames()
	}

	for _, name := range loggerNames {
		if args.LogLevel != nil {
			if err := a.LogFactory.SetLogLevel(name, *args.LogLevel); err != nil {
				return err
			}
		}
		if args.DisplayLevel != nil {
			if err := a.LogFactory.SetDisplayLevel(name, *args.DisplayLevel); err != nil {
				return err
			}
		}
	}
	return nil
}

type LogAndDisplayLevels struct {
	LogLevel     logging.Level `json:"logLevel"`
	DisplayLevel logging.Level `json:"displayLevel"`
}

// See GetLoggerLevel
type GetLoggerLevelArgs struct {
	Secret
	LoggerName string `json:"loggerName"`
}

// See GetLoggerLevel
type GetLoggerLevelReply struct {
	LoggerLevels map[string]LogAndDisplayLevels `json:"loggerLevels"`
}

// GetLogLevel returns the log level and display level of all loggers.
func (a *Admin) GetLoggerLevel(_ *http.Request, args *GetLoggerLevelArgs, reply *GetLoggerLevelReply) error {
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "getLoggerLevels"),
		logging.UserString("loggerName", args.LoggerName),
	)
	reply.LoggerLevels = make(map[string]LogAndDisplayLevels)
	var loggerNames []string
	// Empty name means all loggers
	if len(args.LoggerName) > 0 {
		loggerNames = []string{args.LoggerName}
	} else {
		loggerNames = a.LogFactory.GetLoggerNames()
	}

	for _, name := range loggerNames {
		logLevel, err := a.LogFactory.GetLogLevel(name)
		if err != nil {
			return err
		}
		displayLevel, err := a.LogFactory.GetDisplayLevel(name)
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

// GetConfig returns the config that the node was started with.
func (a *Admin) GetConfig(_ *http.Request, args *Secret, reply *interface{}) error { //nolint:revive
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "getConfig"),
	)
	*reply = a.NodeConfig
	return nil
}

// LoadVMsReply contains the response metadata for LoadVMs
type LoadVMsReply struct {
	// VMs and their aliases which were successfully loaded
	NewVMs map[ids.ID][]string `json:"newVMs"`
	// VMs that failed to be loaded and the error message
	FailedVMs map[ids.ID]string `json:"failedVMs,omitempty"`
}

// LoadVMs loads any new VMs available to the node and returns the added VMs.
func (a *Admin) LoadVMs(r *http.Request, args *Secret, reply *LoadVMsReply) error { //nolint:revive
	a.Log.Debug("API called",
		zap.String("service", "admin"),
		zap.String("method", "loadVMs"),
	)

	ctx := r.Context()
	loadedVMs, failedVMs, err := a.VMRegistry.ReloadWithReadLock(ctx)
	if err != nil {
		return err
	}

	// extract the inner error messages
	failedVMsParsed := make(map[ids.ID]string)
	for vmID, err := range failedVMs {
		failedVMsParsed[vmID] = err.Error()
	}

	reply.FailedVMs = failedVMsParsed
	reply.NewVMs, err = ids.GetRelevantAliases(a.VMManager, loadedVMs)
	return err
}

// See GetNodeSigner
type GetNodeSignerReply struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

func (a *Admin) GetNodeSigner(_ *http.Request, args *Secret, reply *GetNodeSignerReply) error { //nolint:revive
	a.Log.Debug("Admin: GetNodeSigner called")

	rsaPrivKey := a.Config.StakingTLSCert.PrivateKey.(*rsa.PrivateKey)
	privKey := secp256k1.RsaPrivateKeyToSecp256PrivateKey(rsaPrivKey)
	pubKeyBytes := hashing.PubkeyBytesToAddress(privKey.PubKey().SerializeCompressed())
	nodeID, err := ids.ToShortID(pubKeyBytes)
	if err != nil {
		return err
	}

	privKeyStr, err := cb58.Encode(privKey.Serialize())
	if err != nil {
		return err
	}

	reply.PrivateKey = secp256k1.PrivateKeyPrefix + privKeyStr
	reply.PublicKey = nodeID.String()

	return nil
}
