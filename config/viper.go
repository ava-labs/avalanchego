// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// BuildViper returns the viper environment from parsing config file from
// default search paths and any parsed command line flags
func BuildViper(fs *flag.FlagSet, args []string) (*viper.Viper, error) {
	pfs, err := buildPFlagSet(fs)
	if err != nil {
		return nil, err
	}
	if err := pfs.Parse(args); err != nil {
		return nil, err
	}

	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.SetEnvPrefix("avago")
	if err := v.BindPFlags(pfs); err != nil {
		return nil, err
	}

	// load node configs from flags or file, depending on which flags are set
	switch {
	case v.IsSet(ConfigContentKey):
		configContentB64 := v.GetString(ConfigContentKey)
		configBytes, err := base64.StdEncoding.DecodeString(configContentB64)
		if err != nil {
			return nil, fmt.Errorf("unable to decode base64 content: %w", err)
		}

		v.SetConfigType(v.GetString(ConfigContentTypeKey))
		if err := v.ReadConfig(bytes.NewBuffer(configBytes)); err != nil {
			return nil, err
		}

	case v.IsSet(ConfigFileKey):
		filename := os.ExpandEnv(v.GetString(ConfigFileKey))
		v.SetConfigFile(filename)
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	// Config deprecations must be after v.ReadInConfig
	deprecateConfigs(v, fs.Output())
	return v, nil
}

func deprecateConfigs(v *viper.Viper, output io.Writer) {
	for key, message := range deprecatedKeys {
		if v.InConfig(key) {
			fmt.Fprintf(output, "Config %s has been deprecated, %s\n", key, message)
		}
	}
}
