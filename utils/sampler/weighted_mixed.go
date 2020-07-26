// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"errors"
)

var (
	errUnableToSearch = errors.New("unable to search for a value")
)

type weightedMixed struct {
	weighteds []Weighted
}

func (s *weightedMixed) Initialize(weights []uint64) error {
	if len(s.weighteds) == 0 {
		return errUnableToSearch
	}

	for _, weighted := range s.weighteds {
		if err := weighted.Initialize(weights); err != nil {
			return err
		}
	}

	return nil
}

func (s *weightedMixed) StartSearch(value uint64) error {
	if len(s.weighteds) == 0 {
		return errUnableToSearch
	}

	for _, weighted := range s.weighteds {
		if err := weighted.StartSearch(value); err != nil {
			return err
		}
	}
	return nil
}

func (s *weightedMixed) ContinueSearch() (int, bool) {
	for _, weighted := range s.weighteds {
		if index, ok := weighted.ContinueSearch(); ok {
			return index, ok
		}
	}
	return 0, false
}
