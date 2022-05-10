// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
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
		keys    = make([]*PrivateKeySECP256K1R, 0, len(keyStrings))
		log     = logging.NoLog{}
		factory = FactorySECP256K1R{}
	)

	for _, key := range keyStrings {
		privKeyBytes, err := formatting.Decode(formatting.CB58, key)
		log.AssertNoError(err)
		pk, err := factory.ToPrivateKey(privKeyBytes)
		log.AssertNoError(err)
		keys = append(keys, pk.(*PrivateKeySECP256K1R))
	}
	return keys
}
