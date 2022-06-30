// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import "time"

var _ Timestamper = &timestamper{}

type Timestamper interface {
	Timestamp() time.Time
}

type timestamper struct{}

// TODO
func (t *timestamper) Timestamp() time.Time {
	return time.Time{}
}
