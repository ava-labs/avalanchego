// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

type Fetcher struct {
	// tracks which validators were asked for which containers in which requests
	OutstandingRequests Requests

	// Called when bootstrapping is done on a specific chain
	OnFinished func(lastReqID uint32) error
}
