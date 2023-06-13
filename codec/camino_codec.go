// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

const UpgradePrefix = uint64(0xFFFFFFFFFFFF0000)

func BuildUpgradeVersionID(version uint16) uint64 {
	return UpgradePrefix | uint64(version)
}

func GetUpgradeVersion(id uint64) uint16 {
	return uint16(id & 0xFFFF)
}
