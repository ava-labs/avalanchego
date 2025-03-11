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

const kubeConfigEnvVar = "KUBECONFIG"

type KubeconfigVars struct {
	Path    string
	Context string

	docPrefix string
}

func (v *KubeconfigVars) RegisterWithFlag(docPrefix string) {
	v.docPrefix = docPrefix
	v.register(flag.StringVar)
}

func (v *KubeconfigVars) RegisterWithFlagSet(flagSet *pflag.FlagSet, docPrefix string) {
	v.docPrefix = docPrefix
	v.register(flagSet.StringVar)
}

func (v *KubeconfigVars) register(stringVar varFunc[string]) {
	stringVar(
		&v.Path,
		"kubeconfig",
		tmpnet.GetEnvWithDefault(kubeConfigEnvVar, os.ExpandEnv("$HOME/.kube/config")),
		v.docPrefix+fmt.Sprintf(
			"The path to a kubernetes configuration file for the target cluster. Also possible to configure via the %s env variable.",
			kubeConfigEnvVar,
		),
	)
	stringVar(
		&v.Context,
		"kubeconfig-context",
		"",
		v.docPrefix+"The path to a kubernetes configuration file for the target cluster",
	)
}
