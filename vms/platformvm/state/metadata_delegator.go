// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "github.com/ava-labs/avalanchego/database"

type delegatorMetadata struct {
	PotentialReward uint64 `v0:"true"` // originally not parsed via codec but using ad-hoc utility

	StakerStartTime int64 `v1:"true"`
}

func parseDelegatorMetadata(bytes []byte, metadata *delegatorMetadata) error {
	switch len(bytes) {
	case database.Uint64Size:
		// only potential reward was stored
		var err error
		metadata.PotentialReward, err = database.ParseUInt64(bytes)
		if err != nil {
			return err
		}

	default:
		_, err := metadataCodec.Unmarshal(bytes, metadata)
		if err != nil {
			return err
		}
	}
	return nil
}
