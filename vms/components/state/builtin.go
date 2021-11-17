// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errNotIDType     = errors.New("expected ids.ID but got unexpected type")
	errNotStatusType = errors.New("expected choices.Status but got unexpected type")
	errNotTimeType   = errors.New("expected time.Time but got unexpected type")
)

func marshalID(idIntf interface{}) ([]byte, error) {
	if id, ok := idIntf.(ids.ID); ok {
		return id[:], nil
	}
	return nil, errNotIDType
}

func unmarshalID(bytes []byte) (interface{}, error) {
	return ids.ToID(bytes)
}

func marshalStatus(statusIntf interface{}) ([]byte, error) {
	if status, ok := statusIntf.(choices.Status); ok {
		return status.Bytes(), nil
	}
	return nil, errNotStatusType
}

func unmarshalStatus(bytes []byte) (interface{}, error) {
	p := wrappers.Packer{Bytes: bytes}
	status := choices.Status(p.UnpackInt())
	if err := status.Valid(); err != nil {
		return nil, err
	}
	return status, p.Err
}

func marshalTime(timeIntf interface{}) ([]byte, error) {
	if t, ok := timeIntf.(time.Time); ok {
		p := wrappers.Packer{MaxSize: wrappers.LongLen}
		p.PackLong(uint64(t.Unix()))
		return p.Bytes, p.Err
	}
	return nil, errNotTimeType
}

func unmarshalTime(bytes []byte) (interface{}, error) {
	p := wrappers.Packer{Bytes: bytes}
	unixTime := p.UnpackLong()
	return time.Unix(int64(unixTime), 0), nil
}
