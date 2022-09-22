// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"github.com/ava-labs/avalanchego/utils/cb58"
)

func BuildTestKeys() []*PrivateKeySECP256K1R {
	var (
		keyStrings = []string{
			"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
			"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
			"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
			"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
			"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
		}
		keys    = make([]*PrivateKeySECP256K1R, len(keyStrings))
		factory = FactorySECP256K1R{}
	)

	for i, key := range keyStrings {
		privKeyBytes, err := cb58.Decode(key)
		if err != nil {
			panic(err)
		}

		pk, err := factory.ToPrivateKey(privKeyBytes)
		if err != nil {
			panic(err)
		}

		keys[i] = pk.(*PrivateKeySECP256K1R)
	}
	return keys
}
