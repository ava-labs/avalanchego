// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// Proposer list constants
const (
	WindowDuration = 5 * time.Second
	MaxDelay       = 2 * WindowDuration
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
	proposers, err := w.proposerRetriever.GetCurrentProposers()
	if err != nil {
		return MaxDelay, err
	}
	proposerList := proposers.List()
	if !proposers.Contains(validatorID) {
		return MaxDelay, nil
	}
	
	for idx, nodeID := range proposerList {
		if nodeID == validatorID {
			delay := time.Duration(idx + 1) * * WindowDuration
			return delay, nil
		}
	}
	return MaxDelay, nil
}
