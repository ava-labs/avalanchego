// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowtest

type Status int

// [Undecided] means the operation hasn't been decided yet
// [Accepted] means the operation was accepted
// [Rejected] means the operation will never be accepted
const (
	Undecided Status = iota
	Accepted
	Rejected
)

func (s Status) String() string {
	switch s {
	case Undecided:
		return "Undecided"
	case Accepted:
		return "Accepted"
	case Rejected:
		return "Rejected"
	default:
		return "Unknown"
	}
}
