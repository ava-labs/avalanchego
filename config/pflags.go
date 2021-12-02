// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"flag"

	"github.com/spf13/pflag"
)

func deprecateFlags(fs *pflag.FlagSet) error {
	for key, message := range deprecatedKeys {
		if err := fs.MarkDeprecated(key, message); err != nil {
			return err
		}
	}
	return nil
}

// buildFlagSet converts a flag set into a pflag set
func buildPFlagSet(fs *flag.FlagSet) (*pflag.FlagSet, error) {
	pfs := pflag.NewFlagSet(fs.Name(), pflag.ContinueOnError)
	pfs.AddGoFlagSet(fs)

	// Flag deprecations must be before parse
	if err := deprecateFlags(pfs); err != nil {
		return nil, err
	}
	return pfs, nil
}
