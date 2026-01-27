// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/utils/json"
)

type Height json.Uint64

const (
	ProposedHeightJSON = `"proposed"`
	ProposedHeight     = math.MaxUint64
)

var errInvalidHeight = errors.New("invalid height")

func (h Height) MarshalJSON() ([]byte, error) {
	if h == ProposedHeight {
		return []byte(ProposedHeightJSON), nil
	}
	return json.Uint64(h).MarshalJSON()
}

func (h *Height) UnmarshalJSON(b []byte) error {
	// First check for known string values
	switch string(b) {
	case json.Null:
		return nil
	case ProposedHeightJSON:
		*h = ProposedHeight
		return nil
	}

	// Otherwise, unmarshal as a uint64
	if err := (*json.Uint64)(h).UnmarshalJSON(b); err != nil {
		return errInvalidHeight
	}

	// MaxUint64 is reserved for proposed height, so return an error if supplied
	// numerically.
	if uint64(*h) == ProposedHeight {
		*h = 0
		return errInvalidHeight
	}
	return nil
}

func (h Height) IsProposed() bool {
	return h == ProposedHeight
}
