// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	errAlphaPreferenceNotAboveHalfK        = errors.New("alphaPreference <= k/2")
	errAlphaConfidenceBelowAlphaPreference = errors.New("alphaConfidence < alphaPreference")
	errAlphaConfidenceAboveK               = errors.New("k < alphaConfidence")
	errConcurrentRepollsNotPositive        = errors.New("concurrentRepolls <= 0")
	errConcurrentRepollsAboveBeta          = errors.New("beta < concurrentRepolls")
	errOptimalProcessingNotPositive        = errors.New("optimalProcessing <= 0")
	errMaxOutstandingItemsNotPositive      = errors.New("maxOutstandingItems <= 0")
	errMaxItemProcessingTimeNotPositive    = errors.New("maxItemProcessingTime <= 0")
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

// Verify returns nil if the parameters describe a valid initialization.
//
// An initialization is valid if the following conditions are met:
//
// - K/2 < AlphaPreference <= AlphaConfidence <= K
// - 0 < ConcurrentRepolls <= Beta
// - 0 < OptimalProcessing
// - 0 < MaxOutstandingItems
// - 0 < MaxItemProcessingTime
//
// Note: K/2 < K implies that 0 <= K/2, so we don't need an explicit check that
// AlphaPreference is positive.
func (p Parameters) Verify() error {
	var errs []error
	if p.AlphaPreference <= p.K/2 {
		errs = append(errs, fmt.Errorf("%w: k = %d, alphaPreference = %d: %w", ErrParametersInvalid, p.K, p.AlphaPreference, errAlphaPreferenceNotAboveHalfK))
	}
	if p.AlphaConfidence < p.AlphaPreference {
		errs = append(errs, fmt.Errorf("%w: alphaPreference = %d, alphaConfidence = %d: %w", ErrParametersInvalid, p.AlphaPreference, p.AlphaConfidence, errAlphaConfidenceBelowAlphaPreference))
	}
	if p.K < p.AlphaConfidence {
		errs = append(errs, fmt.Errorf("%w: k = %d, alphaConfidence = %d: %w", ErrParametersInvalid, p.K, p.AlphaConfidence, errAlphaConfidenceAboveK))
	}
	if p.ConcurrentRepolls <= 0 {
		errs = append(errs, fmt.Errorf("%w: concurrentRepolls = %d: %w", ErrParametersInvalid, p.ConcurrentRepolls, errConcurrentRepollsNotPositive))
	}
	if p.ConcurrentRepolls > p.Beta {
		errs = append(errs, fmt.Errorf("%w: concurrentRepolls = %d, beta = %d: %w", ErrParametersInvalid, p.ConcurrentRepolls, p.Beta, errConcurrentRepollsAboveBeta))
	}
	if p.OptimalProcessing <= 0 {
		errs = append(errs, fmt.Errorf("%w: optimalProcessing = %d: %w", ErrParametersInvalid, p.OptimalProcessing, errOptimalProcessingNotPositive))
	}
	if p.MaxOutstandingItems <= 0 {
		errs = append(errs, fmt.Errorf("%w: maxOutstandingItems = %d: %w", ErrParametersInvalid, p.MaxOutstandingItems, errMaxOutstandingItemsNotPositive))
	}
	if p.MaxItemProcessingTime <= 0 {
		errs = append(errs, fmt.Errorf("%w: maxItemProcessingTime = %d: %w", ErrParametersInvalid, p.MaxItemProcessingTime, errMaxItemProcessingTimeNotPositive))
	}
	return errors.Join(errs...)
}

func (p Parameters) MinPercentConnectedHealthy() float64 {
	// AlphaConfidence is used here to ensure that the node can still feasibly
	// accept operations. If AlphaPreference were used, committing could be
	// extremely unlikely to happen, even while healthy.
	alphaRatio := float64(p.AlphaConfidence) / float64(p.K)
	return alphaRatio*(1-MinPercentConnectedBuffer) + MinPercentConnectedBuffer
}

type terminationCondition struct {
	alphaConfidence int
	beta            int
}

func newSingleTerminationCondition(alphaConfidence int, beta int) []terminationCondition {
	return []terminationCondition{
		{
			alphaConfidence: alphaConfidence,
			beta:            beta,
		},
	}
}
