// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

const (
	// maxNumParents is the max number of parents a vertex may have
	maxNumParents = 128

	// maxTxsPerVtx is the max number of transactions a vertex may have
	maxTxsPerVtx = 128
)

var (
	errBadVersion       = errors.New("invalid version")
	errBadEpoch         = errors.New("invalid epoch")
	errTooManyparentIDs = fmt.Errorf("vertex contains more than %d parentIDs", maxNumParents)
	errNoOperations     = errors.New("vertex contains no operations")
	errTooManyTxs       = fmt.Errorf("vertex contains more than %d transactions", maxTxsPerVtx)
	errInvalidParents   = errors.New("vertex contains non-sorted or duplicated parentIDs")
	errInvalidTxs       = errors.New("vertex contains non-sorted or duplicated transactions")

	_ StatelessVertex = statelessVertex{}
)

type StatelessVertex interface {
	verify.Verifiable
	ID() ids.ID
	Bytes() []byte

	Version() uint16
	ChainID() ids.ID
	StopVertex() bool
	Height() uint64
	Epoch() uint32
	ParentIDs() []ids.ID
	Txs() [][]byte
}

type statelessVertex struct {
	// This wrapper exists so that the function calls aren't ambiguous
	innerStatelessVertex

	// cache the ID of this vertex
	id ids.ID

	// cache the binary format of this vertex
	bytes []byte
}

func (v statelessVertex) ID() ids.ID      { return v.id }
func (v statelessVertex) Bytes() []byte   { return v.bytes }
func (v statelessVertex) Version() uint16 { return v.innerStatelessVertex.Version }
func (v statelessVertex) ChainID() ids.ID { return v.innerStatelessVertex.ChainID }
func (v statelessVertex) StopVertex() bool {
	return v.innerStatelessVertex.Version == codecVersionWithStopVtx
}
func (v statelessVertex) Height() uint64      { return v.innerStatelessVertex.Height }
func (v statelessVertex) Epoch() uint32       { return v.innerStatelessVertex.Epoch }
func (v statelessVertex) ParentIDs() []ids.ID { return v.innerStatelessVertex.ParentIDs }
func (v statelessVertex) Txs() [][]byte       { return v.innerStatelessVertex.Txs }

type innerStatelessVertex struct {
	Version   uint16   `json:"version"`
	ChainID   ids.ID   `serializeV0:"true" serializeV1:"true" json:"chainID"`
	Height    uint64   `serializeV0:"true" serializeV1:"true" json:"height"`
	Epoch     uint32   `serializeV0:"true" json:"epoch"`
	ParentIDs []ids.ID `serializeV0:"true" serializeV1:"true" len:"128" json:"parentIDs"`
	Txs       [][]byte `serializeV0:"true" len:"128" json:"txs"`
}

func (v innerStatelessVertex) Verify() error {
	// TODO: allow stop vertex issuance
	return v.verify()
}

func (v innerStatelessVertex) verify() error {
	switch {
	case v.Version != codecVersion:
		return errBadVersion
	case v.Epoch != 0:
		return errBadEpoch
	case len(v.ParentIDs) > maxNumParents:
		return errTooManyparentIDs
	case len(v.Txs) == 0:
		return errNoOperations
	case len(v.Txs) > maxTxsPerVtx:
		return errTooManyTxs
	case !ids.IsSortedAndUniqueIDs(v.ParentIDs):
		return errInvalidParents
	case !IsSortedAndUniqueHashOf(v.Txs):
		return errInvalidTxs
	default:
		return nil
	}
}

func (v innerStatelessVertex) verifyStopVertex() error {
	switch {
	case v.Version != codecVersionWithStopVtx:
		return errBadVersion
	case v.Epoch != 0:
		return errBadEpoch
	case len(v.ParentIDs) > maxNumParents:
		return errTooManyparentIDs
	case len(v.Txs) != 0:
		return errTooManyTxs
	case !ids.IsSortedAndUniqueIDs(v.ParentIDs):
		return errInvalidParents
	default:
		return nil
	}
}
