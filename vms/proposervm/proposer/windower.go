// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"sort"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Proposer list constants
const (
	WindowDuration = 5 * time.Second
	MaxDelay       = MaxWindows * WindowDuration
)

var _ Windower = &windower{}

type Windower interface {
	Delay(validatorID ids.NodeID) (time.Duration, error)
}

// windower interfaces with P-Chain and it is responsible for calculating the
// delay for the block submission window of a given validator
type windower struct {
	proposerRetriever Retriever
}

func New(proposerRetriever Retriever) Windower {
	return &windower{	
		proposerRetriever: proposerRetriever,
	}
}

func (w *windower) Delay(validatorID ids.NodeID) (time.Duration, error) {
	proposers := proposerRetriever.GetCurrentProposers()
	proposerList := proposers.List()
	if !proposers.Contains(validatorID) {
		return MaxDelay, nil
	}
	
	for idx, nodeID := range proposerList {
		if nodeID == validatorID {
			return (idx + 1) * WindowDuration, nil
		}
	}
}
