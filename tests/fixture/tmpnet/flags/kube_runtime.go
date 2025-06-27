// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package flags

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	kubeRuntime     = "kube"
	kubeFlagsPrefix = kubeRuntime + "-"
	kubeDocPrefix   = "[kube runtime] "
)

var (
	errKubeNamespaceRequired         = errors.New("--kube-namespace is required")
	errKubeImageRequired             = errors.New("--kube-image is required")
	errKubeMinVolumeSizeRequired     = fmt.Errorf("--kube-volume-size must be >= %d", tmpnet.MinimumVolumeSizeGB)
	errKubeBaseAccessibleURIRequired = errors.New("--kube-base-accessible-uri is required when running outside of cluster")
)

type kubeRuntimeVars struct {
	namespace              string
	image                  string
	volumeSizeGB           uint
	useExclusiveScheduling bool
	schedulingLabelKey     string
	schedulingLabelValue   string
	baseAccessibleURI      string
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
		"",
		kubeDocPrefix+"The label key to use for exclusive scheduling for node selection and toleration",
	)
	stringVar(
		&v.schedulingLabelValue,
		"kube-scheduling-label-value",
		"",
		kubeDocPrefix+"The label value to use for exclusive scheduling for node selection and toleration",
	)
	stringVar(
		&v.baseAccessibleURI,
		"kube-base-accessible-uri",
		"",
		kubeDocPrefix+"The base URI for constructing node URIs when running outside of the cluster hosting nodes",
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
	baseAccessibleURI := v.baseAccessibleURI
	if strings.HasPrefix(v.config.Context, "kind-kind") && len(baseAccessibleURI) == 0 {
		// Use the base uri expected for the kind cluster deployed by tmpnet. Not supplying this as a default
		// ensures that an explicit value is required for non-kind clusters.
		//
		// TODO(marun) Log why this value is being used. This will require passing a log through the call chain.
		baseAccessibleURI = "http://localhost:30791"
	}
	if !tmpnet.IsRunningInCluster() && len(baseAccessibleURI) == 0 {
		return nil, errKubeBaseAccessibleURIRequired
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
		// Strip trailing slashes to simplify path composition
		BaseAccessibleURI: strings.TrimRight(baseAccessibleURI, "/"),
	}, nil
}
