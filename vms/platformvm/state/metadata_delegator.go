// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

type delegatorMetadata struct {
	PotentialReward uint64 `v1:"true"`
	StakerStartTime int64  `v1:"true"`

	txID ids.ID
}

func parseDelegatorMetadata(bytes []byte, metadata *delegatorMetadata) error {
	switch len(bytes) {
	case database.Uint64Size:
		// only potential reward was stored
		var err error
		metadata.PotentialReward, err = database.ParseUInt64(bytes)
		return err

	default:
		_, err := metadataCodec.Unmarshal(bytes, metadata)
		return err
	}
}

func writeDelegatorMetadata(db database.KeyValueWriter, metadata *delegatorMetadata, codecVersion uint16) error {
	// The "0" codec is skipped for [delegatorMetadata]. This is to ensure the
	// [validatorMetadata] codec version is the same as the [delegatorMetadata]
	// codec version.
	//
	// TODO: Cleanup post-Durango activation.
	if codecVersion == 0 {
		return database.PutUInt64(db, metadata.txID[:], metadata.PotentialReward)
	}
	metadataBytes, err := metadataCodec.Marshal(codecVersion, metadata)
	if err != nil {
		return err
	}
	return db.Put(metadata.txID[:], metadataBytes)
}
