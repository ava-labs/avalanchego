// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1

import "github.com/ava-labs/avalanchego/utils/cb58"

func TestKeys() []*PrivateKey {
	var (
		keyStrings = []string{
			"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
			"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
			"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
			"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
			"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
		}
		keys = make([]*PrivateKey, len(keyStrings))
	)

	for i, key := range keyStrings {
		privKeyBytes, err := cb58.Decode(key)
		if err != nil {
			panic(err)
		}

		keys[i], err = ToPrivateKey(privKeyBytes)
		if err != nil {
			panic(err)
		}
	}
	return keys
}
