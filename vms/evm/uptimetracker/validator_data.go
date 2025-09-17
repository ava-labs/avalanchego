// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const codecVersion uint16 = 0

var vdrCodec codec.Manager

func init() {
	vdrCodec = codec.NewManager(math.MaxInt32)
	c := linearcodec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(validatorData{}),

		vdrCodec.RegisterCodec(codecVersion, c),
	)

	if errs.Errored() {
		panic(errs.Err)
	}
}

// The validatorData implementation only allows existing validator's `weight` and `IsActive`
// fields to be updated; all other fields should be constant and if any other field
// changes, the state manager errors and does not update the validator.
//
// The validatorData implementation also assumes NodeIDs are unique in the tracked set.
type validatorData struct {
	UpDuration    time.Duration `serialize:"true"`
	LastUpdated   uint64        `serialize:"true"`
	NodeID        ids.NodeID    `serialize:"true"`
	Weight        uint64        `serialize:"true"`
	StartTime     uint64        `serialize:"true"`
	IsActive      bool          `serialize:"true"`
	IsL1Validator bool          `serialize:"true"`

	validationID ids.ID // database key
}

// parseValidatorData parses the data from the bytes into given validatorData
func parseValidatorData(bytes []byte, data *validatorData) error {
	if len(bytes) != 0 {
		if _, err := vdrCodec.Unmarshal(bytes, data); err != nil {
			return err
		}
	}
	return nil
}

func (v *validatorData) setLastUpdated(t time.Time) {
	v.LastUpdated = uint64(t.Unix())
}

func (v *validatorData) getLastUpdated() time.Time {
	return time.Unix(int64(v.LastUpdated), 0)
}

func (v *validatorData) getStartTime() time.Time {
	return time.Unix(int64(v.StartTime), 0)
}

// constantsAreUnmodified returns true if the constants of this validator have
// not been modified compared to the updated validator.
func (v *validatorData) constantsAreUnmodified(u Validator) bool {
	return v.validationID == u.ValidationID &&
		v.NodeID == u.NodeID &&
		v.IsL1Validator == u.IsL1Validator &&
		v.StartTime == u.StartTimestamp
}
