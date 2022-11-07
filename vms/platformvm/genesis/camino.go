// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

// Camino genesis args
type Camino struct {
	VerifyNodeSignature bool `json:"verifyNodeSignature"`
	LockModeBondDeposit bool `json:"lockModeBondDeposit"`
}
