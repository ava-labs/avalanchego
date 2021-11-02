// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimes

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type stateless struct{}

var errNotReady = errors.New("should not be called")

func UnreadyState() UptimeState {
	return &stateless{}
}

func (u *stateless) GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	return 0, time.Unix(0, 0), errNotReady
}

func (u *stateless) SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error {
	return errNotReady
}
