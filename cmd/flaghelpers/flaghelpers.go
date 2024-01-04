// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flaghelpers

import (
	"os"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const ConfigFileKey = "config-file"

func BuildViper(name string, addFlags func(*pflag.FlagSet), args []string) (*viper.Viper, error) {
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	if addFlags != nil {
		addFlags(fs)
	}
	fs.String(ConfigFileKey, "", "Set the config file location.")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.SetEnvPrefix(name)
	if err := v.BindPFlags(fs); err != nil {
		return nil, err
	}
	if v.IsSet(ConfigFileKey) {
		configFilePath := os.ExpandEnv(v.GetString(ConfigFileKey))
		v.SetConfigFile(configFilePath)
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	return v, nil
}
