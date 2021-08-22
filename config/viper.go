// (c) 2021 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
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
	if v.IsSet(ConfigFileKey) {
		v.SetConfigFile(os.ExpandEnv(v.GetString(ConfigFileKey)))
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
