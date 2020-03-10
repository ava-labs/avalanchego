// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/wrappers"
)

func unmarshalID(bytes []byte) (interface{}, error) {
	return ids.ToID(bytes)
}

func unmarshalStatus(bytes []byte) (interface{}, error) {
	p := wrappers.Packer{Bytes: bytes}
	status := choices.Status(p.UnpackInt())
	if err := status.Valid(); err != nil {
		return nil, err
	}
	return status, p.Err
}

func unmarshalTime(bytes []byte) (interface{}, error) {
	p := wrappers.Packer{Bytes: bytes}
	unixTime := p.UnpackLong()
	return time.Unix(int64(unixTime), 0), nil
}
