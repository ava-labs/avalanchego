// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package protoutils

import (
	"github.com/ava-labs/avalanchego/utils/maybe"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func MaybeToProto(m maybe.Maybe[[]byte]) *pb.MaybeBytes {
	if m.IsNothing() {
		return nil
	}
	return &pb.MaybeBytes{
		Value: m.Value(),
	}
}

func ProtoToMaybe(mb *pb.MaybeBytes) maybe.Maybe[[]byte] {
	if mb == nil {
		return maybe.Nothing[[]byte]()
	}
	return maybe.Some(mb.Value)
}
