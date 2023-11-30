// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"

	"github.com/ava-labs/timestampvm/timestampvm"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	versionKey = "version"
)

func buildFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(timestampvm.Name, flag.ContinueOnError)

	fs.Bool(versionKey, false, "If true, prints Version and quit")

	return fs
}

// getViper returns the viper environment for the plugin binary
func getViper() (*viper.Viper, error) {
	v := viper.New()

	fs := buildFlagSet()
	pflag.CommandLine.AddGoFlagSet(fs)
	pflag.Parse()
	if err := v.BindPFlags(pflag.CommandLine); err != nil {
		return nil, err
	}

	return v, nil
}

func PrintVersion() (bool, error) {
	v, err := getViper()
	if err != nil {
		return false, err
	}

	return v.GetBool(versionKey), nil
}
