// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var errOutNotOwned = errors.New("out doesn't implement fx.Owned interface")

// Returns hash of marshalled bytes of output owner, which can be treated as owner ID.
// [out] must implement fx.Owned interface.
func GetOwnerID(out interface{}) (ids.ID, error) {
	owned, ok := out.(fx.Owned)
	if !ok {
		return ids.Empty, fmt.Errorf("expected fx.Owned but got %T: %w", out, errOutNotOwned)
	}
	owner := owned.Owners()
	ownerBytes, err := txs.Codec.Marshal(txs.Version, owner)
	if err != nil {
		return ids.Empty, fmt.Errorf("couldn't marshal owner: %w", err)
	}
	return hashing.ComputeHash256Array(ownerBytes), nil
}
