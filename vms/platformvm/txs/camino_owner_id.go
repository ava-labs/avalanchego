// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
)

var errOutNotOwned = errors.New("out doesn't implement fx.Owned interface")

// Returns hash of marshalled bytes of owner, which can be treated as owner ID.
func GetOwnerID(owner interface{}) (ids.ID, error) {
	ownerBytes, err := Codec.Marshal(Version, owner)
	if err != nil {
		return ids.Empty, fmt.Errorf("couldn't marshal owner: %w", err)
	}
	return hashing.ComputeHash256Array(ownerBytes), nil
}

// Returns hash of marshalled bytes of output owner, which can be treated as owner ID.
// [out] must implement fx.Owned interface.
func GetOutputOwnerID(out interface{}) (ids.ID, error) {
	owned, ok := out.(fx.Owned)
	if !ok {
		return ids.Empty, fmt.Errorf("expected fx.Owned but got %T: %w", out, errOutNotOwned)
	}
	return GetOwnerID(owned.Owners())
}
