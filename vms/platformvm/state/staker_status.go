// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

const (
	unmodified diffValidatorStatus = iota
	added
	deleted
	modified
)

type diffValidatorStatus uint8

func (s diffValidatorStatus) String() string {
	switch s {
	case unmodified:
		return "unmodified"
	case added:
		return "added"
	case deleted:
		return "deleted"
	case modified:
		return "modified"
	}

	return "invalid validator status"
}
