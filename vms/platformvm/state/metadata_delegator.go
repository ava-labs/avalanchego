// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "github.com/ava-labs/avalanchego/database"

type delegatorMetadata struct {
	PotentialReward uint64
}

func parseDelegatorMetadata(bytes []byte, metadata *delegatorMetadata) error {
	var err error
	metadata.PotentialReward, err = database.ParseUInt64(bytes)
	return err
}
