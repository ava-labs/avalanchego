// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const Version = "v0.1.1"

const (
	ConfigFilePathKey = "config-file"
	LogLevelKey       = "log-level"
	EndpointsKey      = "endpoints"
	MaxFeeCapKey      = "max-fee-cap"
	MaxTipCapKey      = "max-tip-cap"
	WorkersKey        = "workers"
	TxsPerWorkerKey   = "txs-per-worker"
	KeyDirKey         = "key-dir"
	VersionKey        = "version"
	TimeoutKey        = "timeout"
	BatchSizeKey      = "batch-size"
	MetricsPortKey    = "metrics-port"
	MetricsOutputKey  = "metrics-output"
)

var (
	ErrNoEndpoints = errors.New("must specify at least one endpoint")
	ErrNoWorkers   = errors.New("must specify non-zero number of workers")
	ErrNoTxs       = errors.New("must specify non-zero number of txs-per-worker")
)

type Config struct {
	Endpoints     []string      `json:"endpoints"`
	MaxFeeCap     int64         `json:"max-fee-cap"`
	MaxTipCap     int64         `json:"max-tip-cap"`
	Workers       int           `json:"workers"`
	TxsPerWorker  uint64        `json:"txs-per-worker"`
	KeyDir        string        `json:"key-dir"`
	Timeout       time.Duration `json:"timeout"`
	BatchSize     uint64        `json:"batch-size"`
	MetricsPort   uint64        `json:"metrics-port"`
	MetricsOutput string        `json:"metrics-output"`
}

func BuildConfig(v *viper.Viper) (Config, error) {
	c := Config{
		Endpoints:     v.GetStringSlice(EndpointsKey),
		MaxFeeCap:     v.GetInt64(MaxFeeCapKey),
		MaxTipCap:     v.GetInt64(MaxTipCapKey),
		Workers:       v.GetInt(WorkersKey),
		TxsPerWorker:  v.GetUint64(TxsPerWorkerKey),
		KeyDir:        v.GetString(KeyDirKey),
		Timeout:       v.GetDuration(TimeoutKey),
		BatchSize:     v.GetUint64(BatchSizeKey),
		MetricsPort:   v.GetUint64(MetricsPortKey),
		MetricsOutput: v.GetString(MetricsOutputKey),
	}
	if len(c.Endpoints) == 0 {
		return c, ErrNoEndpoints
	}
	if c.Workers == 0 {
		return c, ErrNoWorkers
	}
	if c.TxsPerWorker == 0 {
		return c, ErrNoTxs
	}
	// Note: it's technically valid for the fee/tip cap to be 0, but cannot
	// be less than 0.
	if c.MaxFeeCap < 0 {
		return c, fmt.Errorf("invalid max fee cap %d < 0", c.MaxFeeCap)
	}
	if c.MaxTipCap < 0 {
		return c, fmt.Errorf("invalid max tip cap %d <= 0", c.MaxTipCap)
	}
	return c, nil
}

func BuildViper(fs *pflag.FlagSet, args []string) (*viper.Viper, error) {
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.SetEnvPrefix("evm_simulator")
	if err := v.BindPFlags(fs); err != nil {
		return nil, err
	}

	if v.IsSet(ConfigFilePathKey) {
		v.SetConfigFile(v.GetString(ConfigFilePathKey))
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// BuildFlagSet returns a complete set of flags for simulator
func BuildFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("simulator", pflag.ContinueOnError)
	addSimulatorFlags(fs)
	return fs
}

func addSimulatorFlags(fs *pflag.FlagSet) {
	fs.Bool(VersionKey, false, "Print the version and exit")
	fs.String(ConfigFilePathKey, "", "Specify the config path to use to load a YAML config for the simulator")
	fs.StringSlice(EndpointsKey, []string{"ws://127.0.0.1:9650/ext/bc/C/ws"}, "Specify a comma separated list of RPC Websocket Endpoints (minimum of 1 endpoint)")
	fs.Int64(MaxFeeCapKey, 50, "Specify the maximum fee cap to use for transactions denominated in GWei (must be > 0)")
	fs.Int64(MaxTipCapKey, 1, "Specify the max tip cap for transactions denominated in GWei (must be >= 0)")
	fs.Uint64(TxsPerWorkerKey, 100, "Specify the number of transactions to create per worker (must be > 0)")
	fs.Int(WorkersKey, 1, "Specify the number of workers to create for the simulator (must be > 0)")
	fs.String(KeyDirKey, ".simulator/keys", "Specify the directory to save private keys in (INSECURE: only use for testing)")
	fs.Duration(TimeoutKey, 5*time.Minute, "Specify the timeout for the simulator to complete (0 indicates no timeout)")
	fs.String(LogLevelKey, "info", "Specify the log level to use in the simulator")
	fs.Uint64(BatchSizeKey, 100, "Specify the batchsize for the worker to issue and confirm txs")
	fs.Uint64(MetricsPortKey, 8082, "Specify the port to use for the metrics server")
	fs.String(MetricsOutputKey, "", "Specify the file to write metrics in json format, or empty to write to stdout (defaults to stdout)")
}
