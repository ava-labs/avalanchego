// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import "errors"

const UpgradePrefix = uint64(0xFFFFFFFFFFFF0000)

type UpgradeVersionID uint64

const (
	UpgradeVersion0 UpgradeVersionID = UpgradeVersionID(UpgradePrefix)
	UpgradeVersion1 UpgradeVersionID = UpgradeVersionID(UpgradePrefix | uint64(1))
)

func (id UpgradeVersionID) Version() uint16 {
	return uint16(id & 0xFFFF)
}

func BuildUpgradeVersionID(version uint16) UpgradeVersionID {
	return UpgradeVersionID(UpgradePrefix | uint64(version))
}

var ErrIncompatibleUpgradeVersion = errors.New("incompatible upgrade version")
