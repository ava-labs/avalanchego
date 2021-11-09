// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errNotReady = errors.New("should not be called")

	_ State = stateless{}
)

type stateless struct{}

func UnreadyState() State {
	return stateless{}
}

func (stateless) GetUptime(ids.ShortID) (time.Duration, time.Time, error) {
	return 0, time.Time{}, errNotReady
}

func (stateless) SetUptime(ids.ShortID, time.Duration, time.Time) error {
	return errNotReady
}

func (stateless) GetStartTime(ids.ShortID) (time.Time, error) {
	return time.Time{}, errNotReady
}
