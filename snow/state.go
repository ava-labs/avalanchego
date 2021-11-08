package snow

type State uint8

const (
	Unknown State = iota
	Bootstrapping
	NormalOp
)
