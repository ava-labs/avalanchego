// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package snow

type State uint8

const (
	Unknown State = iota
	FastSyncing
	Bootstrapping
	NormalOp
)

func (st State) String() string {
	switch st {
	case Unknown:
		return "Unknown state"
	case FastSyncing:
		return "Fast syncing state"
	case Bootstrapping:
		return "Bootstrapping state"
	case NormalOp:
		return "Normal operations state"
	default:
		return "Unknown state"
	}
}
