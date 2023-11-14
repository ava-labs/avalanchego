// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

type delegatorMetadata struct {
	PotentialReward uint64 `v0:"true"`
	StakerStartTime int64  `          v1:"true"`

	txID ids.ID
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

func writeDelegatorMetadata(db database.KeyValueWriter, metadata *delegatorMetadata) error {
	metadataBytes, err := metadataCodec.Marshal(v1, metadata)
	if err != nil {
		return err
	}
	return db.Put(metadata.txID[:], metadataBytes)
}
