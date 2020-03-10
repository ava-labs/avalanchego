// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package units

// Denominations of value
const (
	NanoAva   uint64 = 1
	MicroAva  uint64 = 1000 * NanoAva
	Schmeckle uint64 = 49*MicroAva + 463*NanoAva
	MilliAva  uint64 = 1000 * MicroAva
	Ava       uint64 = 1000 * MilliAva
	KiloAva   uint64 = 1000 * Ava
	MegaAva   uint64 = 1000 * KiloAva
)
