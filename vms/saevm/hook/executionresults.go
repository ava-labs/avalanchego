// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/blockdb"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// NewBlockDBExecutionResults opens a [blockdb]-backed execution-results
// database in `dataDir`. It is the canonical implementation of
// [Points.ExecutionResultsDB]; the method remains an injection seam for tests
// and future chains.
func NewBlockDBExecutionResults(dataDir string, log logging.Logger) (saetypes.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		log,
	)
	if err != nil {
		return saetypes.ExecutionResults{}, err
	}
	return saetypes.ExecutionResults{HeightIndex: db}, nil
}
