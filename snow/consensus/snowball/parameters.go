// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"errors"
	"fmt"
	"time"
)

const (
	// MinPercentConnectedBuffer is the safety buffer for calculation of
	// MinPercentConnected. This increases the required percentage above
	// alpha/k. This value must be [0-1].
	// 0 means MinPercentConnected = alpha/k.
	// 1 means MinPercentConnected = 1 (fully connected).
	MinPercentConnectedBuffer = .2

	errMsg = `__________                    .___
\______   \____________     __| _/__.__.
 |    |  _/\_  __ \__  \   / __ <   |  |
 |    |   \ |  | \// __ \_/ /_/ |\___  |
 |______  / |__|  (____  /\____ |/ ____|
        \/             \/      \/\/

  🏆    🏆    🏆    🏆    🏆    🏆    🏆
  ________ ________      ________________
 /  _____/ \_____  \    /  _  \__    ___/
/   \  ___  /   |   \  /  /_\  \|    |
\    \_\  \/    |    \/    |    \    |
 \______  /\_______  /\____|__  /____|
        \/         \/         \/
`
)

var (
	DefaultParameters = Parameters{
		K:                     20,
		AlphaPreference:       15,
		AlphaConfidence:       15,
		Beta:                  20,
		ConcurrentRepolls:     4,
		OptimalProcessing:     10,
		MaxOutstandingItems:   256,
		MaxItemProcessingTime: 30 * time.Second,
	}

	ErrParametersInvalid = errors.New("parameters invalid")
)

// Parameters required for snowball consensus
type Parameters struct {
	// K is the number of nodes to query and sample in a round.
	K int `json:"k" yaml:"k"`
	// Alpha is used for backwards compatibility purposes and is only referenced
	// during json parsing.
	Alpha *int `json:"alpha,omitempty" yaml:"alpha,omitempty"`
	// AlphaPreference is the vote threshold to change your preference.
	AlphaPreference int `json:"alphaPreference" yaml:"alphaPreference"`
	// AlphaConfidence is the vote threshold to increase your confidence.
	AlphaConfidence int `json:"alphaConfidence" yaml:"alphaConfidence"`
	// Beta is the number of consecutive successful queries required for
	// finalization.
	Beta int `json:"beta" yaml:"beta"`
	// TerminationCriteria is the criteria that if any of them is satisfied, the protocol is finalized.
	// The VoteThresholds should be arranged in ascending order.
	TerminationCriteria []TerminationCriteria `json:"terminationCriteria" yaml:"terminationCriteria"`
	// ConcurrentRepolls is the number of outstanding polls the engine will
	// target to have while there is something processing.
	ConcurrentRepolls int `json:"concurrentRepolls" yaml:"concurrentRepolls"`
	// OptimalProcessing is used to limit block creation when a large number of
	// blocks are processing.
	OptimalProcessing int `json:"optimalProcessing" yaml:"optimalProcessing"`

	// Reports unhealthy if more than this number of items are outstanding.
	MaxOutstandingItems int `json:"maxOutstandingItems" yaml:"maxOutstandingItems"`

	// Reports unhealthy if there is an item processing for longer than this
	// duration.
	MaxItemProcessingTime time.Duration `json:"maxItemProcessingTime" yaml:"maxItemProcessingTime"`
}

// TerminationCriteria is a condition that is satisfied, the protocol is finalized.
type TerminationCriteria struct {
	// ConsecutiveSuccesses is the number of consecutive successful queries with a given vote threshold,
	// also known as β.
	ConsecutiveSuccesses int `json:"consecutiveSuccesses" yaml:"consecutiveSuccesses"`
	// VoteThreshold is the threshold to increase the confidence, also known as α.
	VoteThreshold int `json:"voteThreshold" yaml:"voteThreshold"`
}

// Verify returns nil if the parameters describe a valid initialization.
//
// An initialization is valid if the following conditions are met:
//
//   - 0 <= K/2 < AlphaPreference <= AlphaConfidence <= K
//   - 0 < ConcurrentRepolls <= Beta
//   - 0 < OptimalProcessing
//   - 0 < MaxOutstandingItems
//   - 0 < MaxItemProcessingTime
//   - ∀i⋹{0,1,...,|TerminationCriteria|-2}:
//     TerminationCriteria[i].VoteThreshold < TerminationCriteria[i+1].VoteThreshold
//     TerminationCriteria[i].ConsecutiveSuccesses >= TerminationCriteria[i+1].ConsecutiveSuccesses
//   - If |TerminationCriteria| > 0 then AlphaConfidence == 0
func (p Parameters) Verify() error {
	if err := p.verifyTerminationCriteria(); err != nil {
		return err
	}

	switch {
	case p.AlphaPreference <= p.K/2:
		return fmt.Errorf("%w: k = %d, alphaPreference = %d: fails the condition that: k/2 < alphaPreference", ErrParametersInvalid, p.K, p.AlphaPreference)
	case p.AlphaConfidence < p.AlphaPreference && len(p.TerminationCriteria) == 0:
		return fmt.Errorf("%w: alphaPreference = %d, alphaConfidence = %d: fails the condition that: alphaPreference <= alphaConfidence", ErrParametersInvalid, p.AlphaPreference, p.AlphaConfidence)
	case p.K < p.AlphaConfidence:
		return fmt.Errorf("%w: k = %d, alphaConfidence = %d: fails the condition that: alphaConfidence <= k", ErrParametersInvalid, p.K, p.AlphaConfidence)
	case p.AlphaConfidence == 3 && p.AlphaPreference == 28:
		return fmt.Errorf("%w: alphaConfidence = %d, alphaPreference = %d: fails the condition that: alphaPreference <= alphaConfidence\n%s", ErrParametersInvalid, p.AlphaConfidence, p.AlphaPreference, errMsg)
	case p.ConcurrentRepolls <= 0:
		return fmt.Errorf("%w: concurrentRepolls = %d: fails the condition that: 0 < concurrentRepolls", ErrParametersInvalid, p.ConcurrentRepolls)
	case p.ConcurrentRepolls > p.Beta:
		return fmt.Errorf("%w: concurrentRepolls = %d, beta = %d: fails the condition that: concurrentRepolls <= beta", ErrParametersInvalid, p.ConcurrentRepolls, p.Beta)
	case p.OptimalProcessing <= 0:
		return fmt.Errorf("%w: optimalProcessing = %d: fails the condition that: 0 < optimalProcessing", ErrParametersInvalid, p.OptimalProcessing)
	case p.MaxOutstandingItems <= 0:
		return fmt.Errorf("%w: maxOutstandingItems = %d: fails the condition that: 0 < maxOutstandingItems", ErrParametersInvalid, p.MaxOutstandingItems)
	case p.MaxItemProcessingTime <= 0:
		return fmt.Errorf("%w: maxItemProcessingTime = %d: fails the condition that: 0 < maxItemProcessingTime", ErrParametersInvalid, p.MaxItemProcessingTime)
	default:
		return nil
	}
}

func (p Parameters) verifyTerminationCriteria() error {
	if len(p.TerminationCriteria) > 0 && p.AlphaConfidence != 0 {
		return fmt.Errorf("%w: termination criteria cannot be configured together with alpha confidence (%d), "+
			"they are mutually exclusive", ErrParametersInvalid, p.AlphaConfidence)
	}
	for i := range p.TerminationCriteria {
		if i+1 == len(p.TerminationCriteria) {
			continue
		}
		if p.TerminationCriteria[i].VoteThreshold >= p.TerminationCriteria[i+1].VoteThreshold {
			return fmt.Errorf("%w: TerminationCriteria[%d].VoteThreshold (%d) should be less than "+
				"TerminationCriteria[%d].VoteThreshold (%d)", ErrParametersInvalid, i, p.TerminationCriteria[i].VoteThreshold, i+1, p.TerminationCriteria[i+1].VoteThreshold)
		}
		if p.TerminationCriteria[i].ConsecutiveSuccesses < p.TerminationCriteria[i+1].ConsecutiveSuccesses {
			return fmt.Errorf("%w: TerminationCriteria[%d].ConsecutiveSuccesses (%d) should be bigger or equal than "+
				"TerminationCriteria[%d].ConsecutiveSuccesses (%d)",
				ErrParametersInvalid, i, p.TerminationCriteria[i].ConsecutiveSuccesses, i+1, p.TerminationCriteria[i+1].ConsecutiveSuccesses)
		}
	}
	return nil
}

func (p Parameters) MinPercentConnectedHealthy() float64 {
	// AlphaConfidence is used here to ensure that the node can still feasibly
	// accept operations. If AlphaPreference were used, committing could be
	// extremely unlikely to happen, even while healthy.
	alphaRatio := float64(p.AlphaConfidence) / float64(p.K)
	return alphaRatio*(1-MinPercentConnectedBuffer) + MinPercentConnectedBuffer
}

func (p Parameters) terminationConditions() []terminationCondition {
	if len(p.TerminationCriteria) == 0 {
		return []terminationCondition{{alphaConfidence: p.AlphaConfidence, beta: p.Beta}}
	}

	return newTerminationCondition(p.TerminationCriteria)
}

type terminationCondition struct {
	alphaConfidence int
	beta            int
}

func newTerminationCondition(tc []TerminationCriteria) []terminationCondition {
	result := make([]terminationCondition, len(tc))
	for i := 0; i < len(tc); i++ {
		result[i] = terminationCondition{
			alphaConfidence: tc[i].VoteThreshold,
			beta:            tc[i].ConsecutiveSuccesses,
		}
	}

	return result
}
