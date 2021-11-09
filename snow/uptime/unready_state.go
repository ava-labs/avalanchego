// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type stateless struct{}

var errNotReady = errors.New("should not be called")

func UnreadyState() State {
	return &stateless{}
}

func (u *stateless) GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	return 0, time.Time{}, errNotReady
}

func (u *stateless) SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error {
	return errNotReady
}

func (u *stateless) GetStartTime(nodeID ids.ShortID) (time.Time, error) {
	return time.Time{}, errNotReady
}
