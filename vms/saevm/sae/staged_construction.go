package sae

import (
	"sync/atomic"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/libevm/common"
)

// TODO(arr4n) the exhaustruct@v4 linter doesn't support directives on types, so
// every struct literal of the below types has an explicit directive. When
// upgrading to v5, those can be removed in lieu of the pre-populated ones in
// this file.

//exhaustruct:enforce
type last struct {
	accepted, settled atomic.Pointer[blocks.Block]
}

func (l *last) clone() last {
	return last{ //exhaustruct:enforce
		accepted: cloneAtomicPointer(&l.accepted),
		settled:  cloneAtomicPointer(&l.settled),
	}
}

//exhaustruct:enforce
type blockStateFields struct {
	exec              *saexec.Executor
	consensusCritical *syncMap[common.Hash, *blocks.Block]
	preference        atomic.Pointer[blocks.Block]
	last              last
}

func atomicPointerTo[T any](x *T) (p atomic.Pointer[T]) {
	p.Store(x)
	return
}

func cloneAtomicPointer[T any](p *atomic.Pointer[T]) (cp atomic.Pointer[T]) {
	cp.Store(p.Load())
	return
}
