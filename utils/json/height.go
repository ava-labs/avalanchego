// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package json

import "strconv"

type Height struct {
	Numeric    Uint64
	IsProposed bool
}

const ProposedHeightFlag = "proposed"

func (h Height) MarshalJSON() ([]byte, error) {
	if h.IsProposed {
		return []byte(`"` + ProposedHeightFlag + `"`), nil
	}
	return []byte(`"` + strconv.FormatUint(uint64(h.Numeric), 10) + `"`), nil
}

func (h *Height) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == Null {
		return nil
	}
	if str == ProposedHeightFlag {
		h.IsProposed = true
		return nil
	}
	return h.Numeric.UnmarshalJSON(b)
}
