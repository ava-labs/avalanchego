// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/strevm/adaptor"
	"github.com/ava-labs/strevm/sae"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct{}

func (*Factory) New(logger logging.Logger) (interface{}, error) {
	mempoolConfig := legacypool.DefaultConfig
	mempoolConfig.NoLocals = true
	logger.Info("Creating new SAE since genesis")
	res := adaptor.Convert(NewSinceGenesis(sae.Config{
		Hooks:         &Hooks{},
		MempoolConfig: mempoolConfig,
		TrieDBConfig:  triedb.HashDefaults,
	}))

	logger.Info("Successfully created new SAE since genesis")
	return res, nil
}
