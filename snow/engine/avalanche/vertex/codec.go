// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// maxSize is the maximum allowed vertex size. It is necessary to deter DoS
	maxSize = 1 << 20

	// noEpochTransitionsCodecVersion is the codec version that was used when
	// there were no epoch transitions
	noEpochTransitionsCodecVersion = uint16(0)

	// apricotCodecVersion is the codec version that was used when we added
	// epoch transitions
	apricotCodecVersion = uint16(1)
)

var (
	Codec codec.Manager
)

func init() {
	codecV0 := codec.New("serializeV0", maxSize)
	codecV1 := codec.New("serializeV1", maxSize)
	Codec = codec.NewManager(maxSize)

	errs := wrappers.Errs{}
	errs.Add(
		Codec.RegisterCodec(noEpochTransitionsCodecVersion, codecV0),
		Codec.RegisterCodec(apricotCodecVersion, codecV1),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}
