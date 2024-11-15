// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
	"math"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

type Height avajson.Uint64

const (
	ProposedHeightFlag = "proposed"
	ProposedHeight     = math.MaxUint64
)

var errInvalidHeight = errors.New("invalid height")

func (h Height) MarshalJSON() ([]byte, error) {
	if h == ProposedHeight {
		return []byte(`"` + ProposedHeightFlag + `"`), nil
	}
	return avajson.Uint64(h).MarshalJSON()
}

func (h *Height) UnmarshalJSON(b []byte) error {
	if string(b) == ProposedHeightFlag {
		*h = ProposedHeight
		return nil
	}
	err := (*avajson.Uint64)(h).UnmarshalJSON(b)
	if err != nil {
		return errInvalidHeight
	}
	// MaxUint64 is reserved for proposed height
	// return an error if supplied numerically
	if uint64(*h) == ProposedHeight {
		*h = 0
		return errInvalidHeight
	}
	return nil
}

func (h Height) IsProposed() bool {
	return h == ProposedHeight
}
