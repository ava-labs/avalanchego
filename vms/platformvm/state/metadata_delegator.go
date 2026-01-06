// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

type delegatorMetadata struct {
	PotentialReward uint64 `v1:"true"`
	StakerStartTime uint64 `v1:"true"`

	txID ids.ID
}

func parseDelegatorMetadata(bytes []byte, metadata *delegatorMetadata) error {
	var err error
	switch len(bytes) {
	case database.Uint64Size:
		// only potential reward was stored
		metadata.PotentialReward, err = database.ParseUInt64(bytes)
	default:
		_, err = MetadataCodec.Unmarshal(bytes, metadata)
	}
	return err
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
	metadataBytes, err := MetadataCodec.Marshal(codecVersion, metadata)
	if err != nil {
		return err
	}
	return db.Put(metadata.txID[:], metadataBytes)
}
