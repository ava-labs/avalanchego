// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

const (
	URIsKey     = "uris"
	ChainIDsKey = "chain-ids"

	FlagsName = "workload"
	EnvPrefix = "avawl"
)

var (
	errNoURIs      = errors.New("at least one URI must be provided")
	errNoArguments = errors.New("no arguments")
)

type Config struct {
	URIs     []string
	ChainIDs []string
}

func NewConfig(arguments []string) (*Config, error) {
	v, err := parseFlags(arguments)
	if err != nil {
		return nil, err
	}

	c := &Config{
		URIs:     v.GetStringSlice(URIsKey),
		ChainIDs: v.GetStringSlice(ChainIDsKey),
	}
	return c, c.Verify()
}

func (c *Config) Verify() error {
	if len(c.URIs) == 0 {
		return errNoURIs
	}
	return nil
}

func parseFlags(arguments []string) (*viper.Viper, error) {
	if len(arguments) == 0 {
		return nil, errNoArguments
	}

	fs := pflag.NewFlagSet(FlagsName, pflag.ContinueOnError)
	fs.StringSlice(URIsKey, []string{primary.LocalAPIURI}, "URIs of nodes that the workload can communicate with")
	fs.StringSlice(ChainIDsKey, []string{}, "IDs of chains to target for testing")
	if err := fs.Parse(arguments[1:]); err != nil {
		return nil, fmt.Errorf("failed parsing CLI flags: %w", err)
	}

	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(config.DashesToUnderscores)
	v.SetEnvPrefix(EnvPrefix)
	if err := v.BindPFlags(fs); err != nil {
		return nil, fmt.Errorf("failed binding pflags: %w", err)
	}
	return v, nil
}
