// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"crypto/sha256"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"simplex"
	"simplex/record"
	"simplex/wal"
)

func newWal(ctx SimplexChainContext) (*WALInterceptor, error) {
	h := sha256.New()
	h.Write(ctx.NodeID[:])
	h.Write(ctx.ChainID[:])
	walDigest := h.Sum(nil)
	walFileName := fmt.Sprintf("%x.wal", walDigest[:10])

	var epochWAL simplex.WriteAheadLog
	var err error

	epochWAL, err = wal.New(walFileName)
	if err != nil {
		return nil, err
	}

	walInterceptor := &WALInterceptor{
		WriteAheadLog: epochWAL,
	}

	return walInterceptor, nil
}
type WALInterceptor struct {
	Intercept func(digest simplex.Digest)
	simplex.WriteAheadLog
}

func (w *WALInterceptor) Append(data []byte) error {
	if len(data) < 2 {
		return errors.New("data too short, expected at least 2 bytes")
	}

	recordType := binary.BigEndian.Uint16(data[:2])
	recordWithoutType := data[2:]

	if recordType == record.NotarizationRecordType {
		var qr simplex.QuorumRecord
		if _, err := asn1.Unmarshal(recordWithoutType, &qr); err != nil {
			return err
		}

		var vote simplex.ToBeSignedVote
		if err := vote.FromBytes(qr.Vote); err != nil {
			return err
		}

		w.Intercept(vote.Digest)
	}

	return w.WriteAheadLog.Append(data)
}
