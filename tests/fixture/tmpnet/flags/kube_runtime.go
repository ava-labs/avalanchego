// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"errors"
	"flag"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	kubeRuntime     = "kube"
	kubeFlagsPrefix = kubeRuntime + "-"
	kubeDocPrefix   = "[kube runtime] "
)

var (
	errKubeNamespaceRequired = errors.New("--kube-namespace is required")
	errKubeImageRequired     = errors.New("--kube-image is required")
)

type kubeRuntimeVars struct {
	namespace string
	image     string
	config    *KubeconfigVars
}

func (v *kubeRuntimeVars) registerWithFlag() {
	v.config = newKubeconfigFlagVars(kubeDocPrefix)
	v.register(flag.StringVar)
}

func (v *kubeRuntimeVars) registerWithFlagSet(flagSet *pflag.FlagSet) {
	v.config = newKubeconfigFlagSetVars(flagSet, kubeDocPrefix)
	v.register(flagSet.StringVar)
}

func (v *kubeRuntimeVars) register(stringVar varFunc[string]) {
	stringVar(
		&v.namespace,
		"kube-namespace",
		tmpnet.DefaultTmpnetNamespace,
		kubeDocPrefix+"The namespace in the target cluster to create nodes in",
	)
	stringVar(
		&v.image,
		"kube-image",
		"avaplatform/avalanchego:latest",
		kubeDocPrefix+"The name of the docker image to use for creating nodes",
	)
}

func (v *kubeRuntimeVars) getKubeRuntimeConfig() (*tmpnet.KubeRuntimeConfig, error) {
	if len(v.namespace) == 0 {
		return nil, errKubeNamespaceRequired
	}
	if len(v.image) == 0 {
		return nil, errKubeImageRequired
	}
	return &tmpnet.KubeRuntimeConfig{
		ConfigPath:    v.config.Path,
		ConfigContext: v.config.Context,
		Namespace:     v.namespace,
		Image:         v.image,
	}, nil
}
