// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"errors"
	"flag"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	kubeRuntime     = "kube"
	kubeFlagsPrefix = kubeRuntime + "-"
	kubeDocPrefix   = "[kube runtime] "
)

var (
	errKubeNamespaceRequired     = errors.New("--kube-namespace is required")
	errKubeImageRequired         = errors.New("--kube-image is required")
	errKubeMinVolumeSizeRequired = fmt.Errorf("--kube-volume-size must be >= %d", tmpnet.MinimumVolumeSizeGB)
)

type kubeRuntimeVars struct {
	namespace              string
	image                  string
	volumeSizeGB           uint
	useExclusiveScheduling bool
	schedulingLabelKey     string
	schedulingLabelValue   string
	config                 *KubeconfigVars
}

func (v *kubeRuntimeVars) registerWithFlag() {
	v.config = newKubeconfigFlagVars(kubeDocPrefix)
	v.register(flag.StringVar, flag.UintVar, flag.BoolVar)
}

func (v *kubeRuntimeVars) registerWithFlagSet(flagSet *pflag.FlagSet) {
	v.config = newKubeconfigFlagSetVars(flagSet, kubeDocPrefix)
	v.register(flagSet.StringVar, flagSet.UintVar, flagSet.BoolVar)
}

func (v *kubeRuntimeVars) register(stringVar varFunc[string], uintVar varFunc[uint], boolVar varFunc[bool]) {
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
	uintVar(
		&v.volumeSizeGB,
		"kube-volume-size",
		tmpnet.MinimumVolumeSizeGB,
		kubeDocPrefix+fmt.Sprintf(
			"The size in gigabytes of the PeristentVolumeClaim to create for the data directory of each node. Value must be >= %d.",
			tmpnet.MinimumVolumeSizeGB,
		),
	)
	boolVar(
		&v.useExclusiveScheduling,
		"kube-use-exclusive-scheduling",
		false,
		kubeDocPrefix+"Whether to schedule each AvalancheGo node to a dedicated Kubernetes node",
	)
	stringVar(
		&v.schedulingLabelKey,
		"kube-scheduling-label-key",
		"purpose",
		kubeDocPrefix+"The label key to use for exclusive scheduling for node selection and toleration",
	)
	stringVar(
		&v.schedulingLabelValue,
		"kube-scheduling-label-value",
		"higher-spec",
		kubeDocPrefix+"The label value to use for exclusive scheduling for node selection and toleration",
	)
}

func (v *kubeRuntimeVars) getKubeRuntimeConfig() (*tmpnet.KubeRuntimeConfig, error) {
	if len(v.namespace) == 0 {
		return nil, errKubeNamespaceRequired
	}
	if len(v.image) == 0 {
		return nil, errKubeImageRequired
	}
	if v.volumeSizeGB < tmpnet.MinimumVolumeSizeGB {
		return nil, errKubeMinVolumeSizeRequired
	}
	if v.useExclusiveScheduling && (len(v.schedulingLabelKey) == 0 || len(v.schedulingLabelValue) == 0) {
		return nil, errKubeSchedulingLabelRequired
	}
	return &tmpnet.KubeRuntimeConfig{
		ConfigPath:             v.config.Path,
		ConfigContext:          v.config.Context,
		Namespace:              v.namespace,
		Image:                  v.image,
		VolumeSizeGB:           v.volumeSizeGB,
		UseExclusiveScheduling: v.useExclusiveScheduling,
		SchedulingLabelKey:     v.schedulingLabelKey,
		SchedulingLabelValue:   v.schedulingLabelValue,
	}, nil
}
