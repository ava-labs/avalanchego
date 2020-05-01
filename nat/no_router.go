// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"errors"
	"net"
	"time"
)

var (
	errNoRouter = errors.New("no nat enabled router was discovered")
)

type noRouter struct{}

func (noRouter) MapPort(_ NetworkProtocol, _, _ uint16, _ string, _ time.Duration) error {
	return errNoRouter
}

func (noRouter) UnmapPort(_ NetworkProtocol, _, _ uint16) error {
	return errNoRouter
}

func (noRouter) IP() (net.IP, error) {
	return nil, errNoRouter
}
