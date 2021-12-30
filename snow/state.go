package snow

import "errors"

type State uint8

var ErrUnknownState = errors.New("unknown node state")

const (
	Undefined State = iota
	Bootstrapping
	NormalOp
)

func (st State) String() string {
	switch st {
	case Undefined:
		return "Undefined state"
	case Bootstrapping:
		return "Bootstrapping state"
	case NormalOp:
		return "Normal operations state"
	default:
		return "Unknown state"
	}
}
