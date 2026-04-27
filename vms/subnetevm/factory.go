// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct{}

func (*Factory) New(logger logging.Logger) (interface{}, error) {
	mempoolConfig := legacypool.DefaultConfig
	mempoolConfig.NoLocals = true
	logger.Info("Creating new SAE VM")
	return adaptor.Convert(New(sae.Config{
		MempoolConfig: mempoolConfig,
		DBConfig: saedb.Config{
			TrieDBConfig: triedb.HashDefaults,
		},
	})), nil
}
