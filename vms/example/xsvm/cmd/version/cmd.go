// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/example/xsvm"
)

const format = `%s:
  VMID:           %s
  Version:        %s
  Plugin Version: %d
`

func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Prints out the version",
		RunE:  versionFunc,
	}
}

func versionFunc(*cobra.Command, []string) error {
	fmt.Printf(
		format,
		xsvm.Name,
		xsvm.ID,
		xsvm.Version,
		version.RPCChainVMProtocol,
	)
	return nil
}
