// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package test

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
)

var (
	factory    = secp256k1.Factory{}
	keyStrings = [15]string{
		// M8zmfounv8MW1HA8Qm6XFZdDQZqJWANd9
		// P-kopernikus1mnj6djf9seyzchmezp398t6sd6paf3l7vdkdxj
		"2RU2p9FWajV11q8jygX75JsxtGLtSKf4NabKiyn9gxDTepXSQx",

		// HPyTCTCEnecxZdJkx76KqhRtTEKsYEsJH
		// P-kopernikus1k0dynpf6cdy7pf7zulzernx8vcufs9c8qr8yn3
		"2iQFSmszgHTuu2QcUxMxAtftswWEGwpPanktBS9sHdaQTxVMmT",

		// NnCX9RiZyouuy3sqhhqyhsYxgNrS3nksv
		// P-kopernikus1amn0n4ewlt0cv3t74yefq7205gp8k3cgpqdh5a
		"79wr5wXE2DQsfHoCNr52CpZFdHzGdrr3S54urnJLWPJ4Bp8zk",

		// CGQp7TRvTRA8VbWzgUpusQVXxowb29JQv
		// P-kopernikus10wfcqgnv2x4flaffppaa4k7n5tnv4nqrknpzv4
		"2cNLiuBPxyQh67c7z8LPTN4bAEEv9537gRw6iZM53uCYvbhQYP",

		// 3tTynNWQnmqj6ByWHFCyjZePghou5EZZp
		// P-kopernikus1r74lercgu4q5uvyejkwtet52gmlcf0n4y56gp4
		"2BBN9Q4snj5epZFDL2uJ8UdMM8ZXhDB3Uv75PE9B3EvsCWxp3P",

		// FMrn64VM6PsXTRfjZnBPQiD5Ac6bcZvDQ
		// P-kopernikus1nkp6rklrnuq9p6v5v08znlt9ypptwydexx7nj8
		"2HQf8pxLyQBzwbv5nXmiEGifQj8cHpNLqc5FUJqz6RvAB2uGan",

		// 6z7b1FL4hL2xdsPZNgQXDknvckwj1g19G
		// P-kopernikus1gxjavv27ktztleujqxtmt43pj8vycq2v35sxar
		"4pCbKXj1c14SJ3fbp63V5t4LjKhWpzdk5irsvHbBpfQvyveDF",

		// Ehqihn73xgcN3yrVq6BfYC24SfqCYGhrp
		// P-kopernikus1jef0zdeard9y6x0w2p2u3l7d8pzagj95dcqlw3
		"kk44gYKJJ9t2XpJpFcTU85RGPFh82SyBQcADnS7brD1dcWDz",

		// LYiXSh3cMs35KuDraxniYD3jzBenSmyc4
		// P-kopernikus16e5l92uflfxwj2qu98js8aahej55p4qankvua6
		"v5SXxYzQTRceLT7JK9Wsc8fyD7dGzbfmM8FF25gx1H4Hagjyz",

		// KLHZigsMLBewD7S4FQiEouqcjGNJG8KBb
		// P-kopernikus1eytlayd886mu2e7cn5cfaehan4nc4uk8qnyh3g
		"2oesbvmu6qH2jTQFLGa2WviHqdyLB1ho8MfjbwvVnbLfKkN3dC",

		// 81TSi86tr2a79qWB8HrE1dJCQm9pGzi7G
		// P-kopernikus1fn00g3hvklp2p9hp0xy6k3ms7sfn6lh6nptrg6
		"2JggJvXSRDJh1snCfW5GU544efrK9zGYaRqordMzVqRTGGDQCC",

		// PzQU89jDUF2HMoR59pYaaZkNTst58dEpe
		// P-kopernikus1lshpz83w497evpvltd9ce934gsypxnqmnywx0k
		"Sf2gP6Nvz7TvwRQ4HJxFgaCYpFtQsEU9FeEjiyPQhrPkpQLnq",

		// Jsne4bwp6PqfgskhqrREsPxYwd7jzpwYh
		// P-kopernikus1cs2s3tly4cjdjke9wxskzz6w8hfp9judaf5c42
		"wEEa2RuGhbdMPvZKbLEAk7tX5eAXxMmCvmEFYVGUeDVxovpGD",

		// GpgZK1DJxYi7pzk4VXUvWymHRxBPdk8Jq
		// P-kopernikus14k882gdfjle7838x44senj9280n6v8c8vw885s
		"23QCog2yL8w5YEPipLYmLQkTyGAw151LCT8J4KqsRY62X74Adr",

		// J9dL2PTTuyG4skUNSHVQBBkFtbsiVQ8wc
		// P-kopernikus1hsdu4pw5t24ydu0nvqfew372tzszvzkn0j90h3
		"2rHGyJwvatTcUFNYbbNJjRLNk8yZukG4AjiWy8H3awGFn2Lfvm",
	}

	fundedKeyStrings = [5]string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
		"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
		"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
	}

	// those keys were generating from staking/local/ keys & certificates
	// see camino_keys_test.go TestGetNodeKeysAndIDs func
	nodeKeyStrings = [5]string{
		// NodeID-AK7sPBsZM9rQwse23aLhEEBPHZD5gkLrL
		"26ksbvjbz8jUTtzbCm3MYobKcDh22QPuPQX5dj2faQdR63TRdM",

		// NodeID-D1LbWvUf9iaeEyUbTYYtYq4b7GaYR5tnJ
		"2ZW6HUePBW2dP7dBGa5stjXe1uvK9LwEgrjebDwXEyL5bDMWWS",

		// NodeID-PM2LqrGsxudhZSP49upMonevbQvnvAciv
		"2tMmHrX7G7SeuuQ5kWbKWPq1ZaGZMYyFoggd7NgmPn5Z7Henmd",

		// NodeID-5ZUdznHckQcqucAnNf3vzXnPF97tfRtfn
		"KpWQv31KtNbtvBPhN3YFk3bmmkuD5Vtv6op41DBjCvZhnPXsb",

		// NodeID-EoYFkbokZEukfWrUovo74YkTFnAMaqTG7
		"Vhw1gdFvJ941yrHXTZdf4x2BLZNSMqGJ4X1kWiiL4XepzHzmG",
	}

	Keys             []*secp256k1.PrivateKey
	KeysBech32       []string
	FundedKeys       []*secp256k1.PrivateKey
	FundedKeysBech32 []string
	FundedNodeKeys   []*secp256k1.PrivateKey
	FundedNodeIDs    []ids.NodeID
)

func init() {
	Keys, KeysBech32 = keysAndAddresses(keyStrings[:])
	FundedKeys, FundedKeysBech32 = keysAndAddresses(fundedKeyStrings[:])
	FundedNodeKeys, _ = keysAndAddresses(nodeKeyStrings[:])
	FundedNodeIDs = make([]ids.NodeID, len(FundedNodeKeys))
	for i := range FundedNodeKeys {
		FundedNodeIDs[i] = ids.NodeID(FundedNodeKeys[i].Address())
	}
}

func keysAndAddresses(keyStrings []string) ([]*secp256k1.PrivateKey, []string) {
	keys := make([]*secp256k1.PrivateKey, len(keyStrings))
	addresses := make([]string, len(keyStrings))
	for i, keyStr := range keyStrings {
		privKeyBytes, err := cb58.Decode(keyStr)
		if err != nil {
			panic(err)
		}

		keys[i], err = factory.ToPrivateKey(privKeyBytes)
		if err != nil {
			panic(err)
		}
		addresses[i], err = address.FormatBech32(constants.UnitTestHRP, keys[i].Address().Bytes())
		if err != nil {
			panic(err)
		}
	}
	return keys, addresses
}
