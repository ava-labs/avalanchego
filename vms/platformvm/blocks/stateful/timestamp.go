// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import "time"

var _ timestampGetter = &timestampGetterImpl{}

type timestampGetter interface {
	GetTimestamp() time.Time
}

type timestampGetterImpl struct{}

// TODO
func (t *timestampGetterImpl) GetTimestamp() time.Time {
	return time.Time{}
}
