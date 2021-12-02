// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

type Fetcher struct {
	// number of containers fetched so far
	NumFetched uint32

	// tracks which validators were asked for which containers in which requests
	OutstandingRequests Requests

	// Called when bootstrapping is done
	OnFinished func() error
}
