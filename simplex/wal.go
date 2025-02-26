package simplex

import (
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"simplex"
	"simplex/record"
)

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
