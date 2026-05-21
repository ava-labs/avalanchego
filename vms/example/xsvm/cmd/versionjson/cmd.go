// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package versionjson

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/utils/constants"
	"github.com/MetalBlockchain/metalgo/version"
	"github.com/MetalBlockchain/metalgo/vms/example/xsvm"
)

type vmVersions struct {
	Name       string `json:"name"`
	VMID       ids.ID `json:"vmid"`
	Version    string `json:"version"`
	RPCChainVM uint64 `json:"rpcchainvm"`
}

func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "version-json",
		Short: "Prints out the version in json format",
		RunE:  versionFunc,
	}
}

func versionFunc(*cobra.Command, []string) error {
	versions := vmVersions{
		Name:       constants.XSVMName,
		VMID:       constants.XSVMID,
		Version:    xsvm.Version,
		RPCChainVM: uint64(version.RPCChainVMProtocol),
	}
	jsonBytes, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal versions: %w", err)
	}
	fmt.Println(string(jsonBytes))
	return nil
}
