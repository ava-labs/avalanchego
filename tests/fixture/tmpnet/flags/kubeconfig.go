// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	v.register(flag.StringVar, docPrefix)
	return v
}

// NewKubeconfigFlagSetVars registers kubeconfig flag variables for pflag
func NewKubeconfigFlagSetVars(flagSet *pflag.FlagSet) *KubeconfigVars {
	return newKubeconfigFlagSetVars(flagSet, "")
}

// internal method enabling configuration of the doc prefix
func newKubeconfigFlagSetVars(flagSet *pflag.FlagSet, docPrefix string) *KubeconfigVars {
	v := &KubeconfigVars{}
	v.register(flagSet.StringVar, docPrefix)
	return v
}

func (v *KubeconfigVars) register(stringVar varFunc[string], docPrefix string) {
	// the default kubeConfig path is set to empty to allow for the use of a projected
	// token when running in-cluster
	var defaultKubeConfigPath string
	if !tmpnet.IsRunningInCluster() {
		defaultKubeConfigPath = os.ExpandEnv("$HOME/.kube/config")
	}

	stringVar(
		&v.Path,
		"kubeconfig",
		tmpnet.GetEnvWithDefault(KubeconfigPathEnvVar, defaultKubeConfigPath),
		docPrefix+fmt.Sprintf(
			"The path to a kubernetes configuration file for the target cluster. Also possible to configure via the %s env variable.",
			KubeconfigPathEnvVar,
		),
	)
	stringVar(
		&v.Context,
		"kubeconfig-context",
		"",
		docPrefix+"The optional kubeconfig context to use",
	)
}
