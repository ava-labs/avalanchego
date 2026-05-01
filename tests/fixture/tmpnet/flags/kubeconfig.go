// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const KubeconfigPathEnvVar = "KUBECONFIG"

type KubeconfigVars struct {
	Path    string
	Context string
}

// NewKubeconfigFlagVars registers kubeconfig flag variables for stdlib flag
func NewKubeconfigFlagVars() *KubeconfigVars {
	return newKubeconfigFlagVars("")
}

// internal method enabling configuration of the doc prefix
func newKubeconfigFlagVars(docPrefix string) *KubeconfigVars {
	v := &KubeconfigVars{}
	v.registerStdlib(flag.CommandLine, docPrefix)
	return v
}

// NewKubeconfigFlagSetVars registers kubeconfig flag variables for pflag
func NewKubeconfigFlagSetVars(flagSet *pflag.FlagSet) *KubeconfigVars {
	return newKubeconfigFlagSetVars(flagSet, "")
}

// internal method enabling configuration of the doc prefix
func newKubeconfigFlagSetVars(flagSet *pflag.FlagSet, docPrefix string) *KubeconfigVars {
	v := &KubeconfigVars{}
	v.registerPFlag(flagSet, docPrefix)
	return v
}

func defaultKubeconfigPath() string {
	// the default kubeConfig path is set to empty to allow for the use of a projected
	// token when running in-cluster
	if tmpnet.IsRunningInCluster() {
		return ""
	}
	return tmpnet.GetEnvWithDefault(KubeconfigPathEnvVar, os.ExpandEnv("$HOME/.kube/config"))
}

func (v *KubeconfigVars) registerStdlib(flagSet *flag.FlagSet, docPrefix string) {
	defaultPath := defaultKubeconfigPath()
	if existing := flagSet.Lookup("kubeconfig"); existing != nil {
		v.Path = existing.Value.String()
		if v.Path == "" {
			v.Path = defaultPath
		}
	} else {
		flagSet.StringVar(
			&v.Path,
			"kubeconfig",
			defaultPath,
			docPrefix+fmt.Sprintf(
				"The path to a kubernetes configuration file for the target cluster. Also possible to configure via the %s env variable.",
				KubeconfigPathEnvVar,
			),
		)
	}
	if existing := flagSet.Lookup("kubeconfig-context"); existing != nil {
		v.Context = existing.Value.String()
	} else {
		flagSet.StringVar(
			&v.Context,
			"kubeconfig-context",
			"",
			docPrefix+"The optional kubeconfig context to use",
		)
	}
}

func (v *KubeconfigVars) registerPFlag(flagSet *pflag.FlagSet, docPrefix string) {
	defaultPath := defaultKubeconfigPath()
	if existing := flagSet.Lookup("kubeconfig"); existing != nil {
		v.Path = existing.Value.String()
		if v.Path == "" {
			v.Path = defaultPath
		}
	} else {
		flagSet.StringVar(
			&v.Path,
			"kubeconfig",
			defaultPath,
			docPrefix+fmt.Sprintf(
				"The path to a kubernetes configuration file for the target cluster. Also possible to configure via the %s env variable.",
				KubeconfigPathEnvVar,
			),
		)
	}
	if existing := flagSet.Lookup("kubeconfig-context"); existing != nil {
		v.Context = existing.Value.String()
	} else {
		flagSet.StringVar(
			&v.Context,
			"kubeconfig-context",
			"",
			docPrefix+"The optional kubeconfig context to use",
		)
	}
}
