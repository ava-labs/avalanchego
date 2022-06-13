// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements static handlers tests for avm and platformvm
package statichandlers

import (
	"context"
	"time"

	"github.com/onsi/gomega"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/e2e"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var _ = ginkgo.Describe("[StaticHandlers]", func() {
	ginkgo.It("can make calls to avm static api", func() {
		addrMap := map[string]string{}
		for _, addrStr := range []string{
			"A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy",
			"6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv",
			"6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa",
			"Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7",
		} {
			b, err := formatting.Decode(formatting.CB58, addrStr)
			gomega.Expect(err).Should(gomega.BeNil())
			addrMap[addrStr], err = address.FormatBech32(constants.NetworkIDToHRP[constants.LocalID], b)
			gomega.Expect(err).Should(gomega.BeNil())
		}
		avmArgs := avm.BuildGenesisArgs{
			Encoding: formatting.Hex,
			GenesisData: map[string]avm.AssetDefinition{
				"asset1": {
					Name:         "myFixedCapAsset",
					Symbol:       "MFCA",
					Denomination: 8,
					InitialState: map[string][]interface{}{
						"fixedCap": {
							avm.Holder{
								Amount:  100000,
								Address: addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
							},
							avm.Holder{
								Amount:  100000,
								Address: addrMap["6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv"],
							},
							avm.Holder{
								Amount:  json.Uint64(50000),
								Address: addrMap["6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa"],
							},
							avm.Holder{
								Amount:  json.Uint64(50000),
								Address: addrMap["Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7"],
							},
						},
					},
				},
				"asset2": {
					Name:   "myVarCapAsset",
					Symbol: "MVCA",
					InitialState: map[string][]interface{}{
						"variableCap": {
							avm.Owners{
								Threshold: 1,
								Minters: []string{
									addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
									addrMap["6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv"],
								},
							},
							avm.Owners{
								Threshold: 2,
								Minters: []string{
									addrMap["6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa"],
									addrMap["Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7"],
								},
							},
						},
					},
				},
				"asset3": {
					Name: "myOtherVarCapAsset",
					InitialState: map[string][]interface{}{
						"variableCap": {
							avm.Owners{
								Threshold: 1,
								Minters: []string{
									addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
								},
							},
						},
					},
				},
			},
		}
		uris := e2e.GetURIs()
		gomega.Expect(uris).ShouldNot(gomega.BeEmpty())
		staticClient := avm.NewStaticClient(uris[0])
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		resp, err := staticClient.BuildGenesis(ctx, &avmArgs)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(resp.Bytes).Should(gomega.Equal("0x0000000000030006617373657431000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000f6d794669786564436170417373657400044d4643410800000001000000000000000400000007000000000000c350000000000000000000000001000000013f78e510df62bc48b0829ec06d6a6b98062d695300000007000000000000c35000000000000000000000000100000001c54903de5177a16f7811771ef2f4659d9e8646710000000700000000000186a0000000000000000000000001000000013f58fda2e9ea8d9e4b181832a07b26dae286f2cb0000000700000000000186a000000000000000000000000100000001645938bb7ae2193270e6ffef009e3664d11e07c10006617373657432000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000d6d79566172436170417373657400044d5643410000000001000000000000000200000006000000000000000000000001000000023f58fda2e9ea8d9e4b181832a07b26dae286f2cb645938bb7ae2193270e6ffef009e3664d11e07c100000006000000000000000000000001000000023f78e510df62bc48b0829ec06d6a6b98062d6953c54903de5177a16f7811771ef2f4659d9e864671000661737365743300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000126d794f7468657256617243617041737365740000000000000100000000000000010000000600000000000000000000000100000001645938bb7ae2193270e6ffef009e3664d11e07c1279fa028"))
	})

	ginkgo.It("can make calls to platformvm static api", func() {
		keys := []*crypto.PrivateKeySECP256K1R{}
		factory := crypto.FactorySECP256K1R{}
		for _, key := range []string{
			"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
			"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
			"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
			"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
			"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
		} {
			privKeyBytes, err := formatting.Decode(formatting.CB58, key)
			gomega.Expect(err).Should(gomega.BeNil())
			pk, err := factory.ToPrivateKey(privKeyBytes)
			gomega.Expect(err).Should(gomega.BeNil())
			keys = append(keys, pk.(*crypto.PrivateKeySECP256K1R))
		}

		genesisUTXOs := make([]platformvm.APIUTXO, len(keys))
		hrp := constants.NetworkIDToHRP[constants.UnitTestID]
		for i, key := range keys {
			id := key.PublicKey().Address()
			addr, err := address.FormatBech32(hrp, id.Bytes())
			gomega.Expect(err).Should(gomega.BeNil())
			genesisUTXOs[i] = platformvm.APIUTXO{
				Amount:  json.Uint64(50000 * units.MilliAvax),
				Address: addr,
			}
		}

		genesisValidators := make([]platformvm.APIPrimaryValidator, len(keys))
		for i, key := range keys {
			id := key.PublicKey().Address()
			addr, err := address.FormatBech32(hrp, id.Bytes())
			gomega.Expect(err).Should(gomega.BeNil())
			genesisValidators[i] = platformvm.APIPrimaryValidator{
				APIStaker: platformvm.APIStaker{
					StartTime: json.Uint64(time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC).Unix()),
					EndTime:   json.Uint64(time.Date(1997, 1, 30, 0, 0, 0, 0, time.UTC).Unix()),
					NodeID:    ids.NodeID(id),
				},
				RewardOwner: &platformvm.APIOwner{
					Threshold: 1,
					Addresses: []string{addr},
				},
				Staked: []platformvm.APIUTXO{{
					Amount:  json.Uint64(10000),
					Address: addr,
				}},
				DelegationFee: reward.PercentDenominator,
			}
		}

		buildGenesisArgs := platformvm.BuildGenesisArgs{
			NetworkID:     json.Uint32(constants.UnitTestID),
			AvaxAssetID:   ids.ID{'a', 'v', 'a', 'x'},
			UTXOs:         genesisUTXOs,
			Validators:    genesisValidators,
			Chains:        nil,
			Time:          json.Uint64(time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC).Unix()),
			InitialSupply: json.Uint64(360 * units.MegaAvax),
			Encoding:      formatting.CB58,
		}

		uris := e2e.GetURIs()
		gomega.Expect(uris).ShouldNot(gomega.BeEmpty())

		staticClient := platformvm.NewStaticClient(uris[0])
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		resp, err := staticClient.BuildGenesis(ctx, &buildGenesisArgs)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(resp.Bytes).Should(gomega.Equal("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"))
	})
})
