// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"errors"
	"fmt"
	"io"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

const (
	// Maximum number of containers IDs that can be fetched at a time
	// in a call to GetContainerRange
	MaxFetchedByRange = 1024
)

var (
	errNoneAccepted   = errors.New("no containers have been accepted")
	errNumToFetchZero = fmt.Errorf("numToFetch must be in [1,%d]", MaxFetchedByRange)
)

// Index indexes containers in their order of acceptance
// Index implements triggers.Acceptor
// Index is thread-safe.
// Index assumes that Accept is called before the container is committed to the
// database of the VM that the container exists in.
type Index interface {
	snow.Acceptor
	GetContainerByIndex(index uint64) (Container, error)
	GetContainerRange(startIndex uint64, numToFetch uint64) ([]Container, error)
	GetLastAccepted() (Container, error)
	GetIndex(containerID ids.ID) (uint64, error)
	GetContainerByID(containerID ids.ID) (Container, error)
	io.Closer
}
