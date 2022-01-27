// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package tests defines common test primitives.
package main

import (
	"io/ioutil"

	"sigs.k8s.io/yaml"
)

// ClusterInfo represents the local cluster information.
type ClusterInfo struct {
	URIs     []string `json:"uris"`
	Endpoint string   `json:"endpoint"`
	PID      int      `json:"pid"`
	LogsDir  string   `json:"logsDir"`
}

const fsModeWrite = 0o600

func (ci ClusterInfo) Save(p string) error {
	ob, err := yaml.Marshal(ci)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(p, ob, fsModeWrite)
}

// LoadClusterInfo loads the cluster info YAML file
// to parse it into "ClusterInfo".
func LoadClusterInfo(p string) (ClusterInfo, error) {
	ob, err := ioutil.ReadFile(p)
	if err != nil {
		return ClusterInfo{}, err
	}
	info := new(ClusterInfo)
	if err = yaml.Unmarshal(ob, info); err != nil {
		return ClusterInfo{}, err
	}
	return *info, nil
}
