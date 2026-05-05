// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/ulimit"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/subnetevm"
)

// Run starts the SAE-based Subnet-EVM as an out-of-process rpcchainvm
// plugin. If `--version` was passed, it prints `versionStr` and exits 0
// before serving (the operator-facing protocol-compatibility check).
func Run(versionStr string) {
	printVersion, err := PrintVersion()
	if err != nil {
		fmt.Printf("couldn't get config: %s\n", err)
		os.Exit(1)
	}
	if printVersion {
		fmt.Println(versionStr)
		os.Exit(0)
	}
	if err := ulimit.Set(ulimit.DefaultFDLimit, logging.NoLog{}); err != nil {
		fmt.Printf("failed to set fd limit correctly due to: %s\n", err)
		os.Exit(1)
	}

	mempoolConfig := legacypool.DefaultConfig
	mempoolConfig.NoLocals = true
	vm := adaptor.Convert(subnetevm.New(sae.Config{
		MempoolConfig: mempoolConfig,
		DBConfig: saedb.Config{
			TrieDBConfig: triedb.HashDefaults,
		},
	}))

	if err := rpcchainvm.Serve(context.Background(), vm); err != nil {
		fmt.Printf("failed to serve rpc chain vm: %s\n", err)
		os.Exit(1)
	}
}
