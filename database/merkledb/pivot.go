package merkledb

import "bytes"

// Pivot represents the status of the reference counting when traversing the tree down to find a node
// and then up when propagating the operations up the tree
// The rules are :
// -- When traversing down
// - If Refs < 2 - the Pivot point has not been reached ( currently the > 1 behaviour is checked on the Nodes )
// - If Refs > 1 && Refs > Current Pivot Refs - a new Pivot point has been reached
// -- When traversing up
// - If Pivot point was reached && Hash == Current Pivot Hash - the Pivot point has been passed
//
type Pivot struct {
	hash    []byte
	reached bool
	refs    int32
	passed  bool
}

// NewPivot returns a new instance of the Pivot
func NewPivot() *Pivot {
	return &Pivot{
		hash:    nil,
		reached: false,
	}
}

// CheckAndSet sets the Pivot if it wasn't reached yet
// and sets the new Hash if its ref count is greater than the current one
func (pv *Pivot) CheckAndSet(hash []byte, refs int32) {
	if !pv.reached {
		pv.reached = true
		pv.hash = hash
		pv.refs = refs
	} else if refs > pv.refs {
		pv.reached = true
		pv.hash = hash
		pv.refs = refs
	}
}

// Copy copies the Pivot
func (pv *Pivot) Copy(pivot *Pivot) {
	if pivot == nil {
		return
	}
	pv.refs = pivot.refs
	pv.hash = pivot.hash
	pv.reached = pivot.reached
	pv.passed = pivot.passed
}

// Equals checks if the Pivot hash is the same as the input hash
func (pv *Pivot) Equals(hash []byte) bool {
	return bytes.Equal(pv.hash, hash)
}
