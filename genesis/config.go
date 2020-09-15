// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// Note that since an Avalanche network has exactly one Platform Chain, and the
// Platform Chain defines the genesis state of the network (who is staking,
// which chains exist, etc.), defining the genesis state of the Platform Chain
// is the same as defining the genesis state of the network.

// Config contains the genesis addresses used to construct a genesis
type Config struct {
	NetworkID                                                   uint32
	MintAddresses, FundedAddresses, StakerIDs                   []string
	ParsedMintAddresses, ParsedFundedAddresses, ParsedStakerIDs []ids.ShortID
	EVMBytes                                                    []byte
	Message                                                     string
}

func (c *Config) init() error {
	expectedHRP := constants.GetHRP(c.NetworkID)
	c.ParsedMintAddresses = nil
	for _, addrStr := range c.MintAddresses {
		hrp, addrBytes, err := formatting.ParseBech32(addrStr)
		if err != nil {
			return err
		}
		if hrp != expectedHRP {
			return fmt.Errorf("wrong HRP, expected %q got %q", expectedHRP, hrp)
		}
		addr, err := ids.ToShortID(addrBytes)
		if err != nil {
			return err
		}
		c.ParsedMintAddresses = append(c.ParsedMintAddresses, addr)
	}
	c.ParsedFundedAddresses = nil
	for _, addrStr := range c.FundedAddresses {
		hrp, addrBytes, err := formatting.ParseBech32(addrStr)
		if err != nil {
			return err
		}
		if hrp != expectedHRP {
			return fmt.Errorf("wrong HRP, expected %q got %q", expectedHRP, hrp)
		}
		addr, err := ids.ToShortID(addrBytes)
		if err != nil {
			return err
		}
		c.ParsedFundedAddresses = append(c.ParsedFundedAddresses, addr)
	}
	c.ParsedStakerIDs = nil
	for _, addrStr := range c.StakerIDs {
		addr, err := ids.ShortFromPrefixedString(addrStr, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		c.ParsedStakerIDs = append(c.ParsedStakerIDs, addr)
	}
	return nil
}

// Hard coded genesis constants
var (
	evmGenesisBytes      = []byte("{\"config\":{\"chainId\":43110,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x730000000000000000000000000000000000000000301460806040526004361061004b5760003560e01c80631e01043914610050578063abb24ba014610092578063b6510bb3146100a9575b600080fd5b61007c6004803603602081101561006657600080fd5b8101908080359060200190929190505050610118565b6040518082815260200191505060405180910390f35b81801561009e57600080fd5b506100a761013b565b005b8180156100b557600080fd5b50610116600480360360808110156100cc57600080fd5b81019080803573ffffffffffffffffffffffffffffffffffffffff16906020019092919080359060200190929190803590602001909291908035906020019092919050505061013e565b005b60003073ffffffffffffffffffffffffffffffffffffffff1682905d9050919050565b5c565b8373ffffffffffffffffffffffffffffffffffffffff1681836108fc8690811502906040516000604051808303818888878c8af69550505050505015801561018a573d6000803e3d6000fd5b505050505056fea2646970667358221220ed2100d6623a884d196eceefabe5e03da4309a2562bb25262f3874f1acb31cd764736f6c634300060a0033\",\"balance\":\"0x0\"},\"751a0b96e1042bee789452ecb20253fba40dbe85\":{\"balance\":\"0x7f0e10af47c1c70000000\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}")
	localEVMGenesisBytes = []byte("{\"config\":{\"chainId\":43110,\"homesteadBlock\":0,\"daoForkBlock\":0,\"daoForkSupport\":true,\"eip150Block\":0,\"eip150Hash\":\"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0\",\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0},\"nonce\":\"0x0\",\"timestamp\":\"0x0\",\"extraData\":\"0x00\",\"gasLimit\":\"0x5f5e100\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"alloc\":{\"0100000000000000000000000000000000000000\":{\"code\":\"0x730000000000000000000000000000000000000000301460806040526004361061004b5760003560e01c80631e01043914610050578063abb24ba014610092578063b6510bb3146100a9575b600080fd5b61007c6004803603602081101561006657600080fd5b8101908080359060200190929190505050610118565b6040518082815260200191505060405180910390f35b81801561009e57600080fd5b506100a761013b565b005b8180156100b557600080fd5b50610116600480360360808110156100cc57600080fd5b81019080803573ffffffffffffffffffffffffffffffffffffffff16906020019092919080359060200190929190803590602001909291908035906020019092919050505061013e565b005b60003073ffffffffffffffffffffffffffffffffffffffff1682905d9050919050565b5c565b8373ffffffffffffffffffffffffffffffffffffffff1681836108fc8690811502906040516000604051808303818888878c8af69550505050505015801561018a573d6000803e3d6000fd5b505050505056fea2646970667358221220ed2100d6623a884d196eceefabe5e03da4309a2562bb25262f3874f1acb31cd764736f6c634300060a0033\",\"balance\":\"0x0\"},\"751a0b96e1042bee789452ecb20253fba40dbe85\":{\"balance\":\"0x83068134c1ffd53800000\"}},\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}")
	EverestConfig        = Config{
		NetworkID:     constants.EverestID,
		MintAddresses: []string{},
		FundedAddresses: []string{
			"everest182njwsuws28ualy459kevrqsea60q2fuu5a0eg",
			"everest1zmnhjp4tpwgswup2h88p5xk68wcuwu84skzsn5",
			"everest1kws7vz6sx97muqd47vgfcee089uxrmvxn38dvd",
			"everest188ln97cvvuwwq53w950ftwcze23y4zxp44mwws",
			"everest1n65k8ku5y0u5u5r0uj9pjp8jngl7us5trnd5e8",
			"everest1dmk86nt3t5k9a4gg59v53hmasnt6zccv9v0stm",
			"everest1xttsgfp2rxd6uy49mwq3ddndq6gwg2lkkkhp3x",
			"everest1az7r2jgp3n8pvdef7zmjezpdmsdrejrracmxxy",
			"everest1htq0cjv9cxegd072zdstslltufsxmnn3qcye5u",
			"everest1xjd2mumr6d95p0jpy42895q98fsfq3l57m0de4",
			"everest175s34288jhmuqldy4sxyj5xttn8e6kkpmk76pd",
			"everest1fm6em48f89tunnek5u5w46ycjfvmn65zjm7wwm",
			"everest1ftxr08f5yqh6nd4qcsag6fcyvnefznrltufk8d",
			"everest18g2ysqpuuumtv2sacxcs5nt4xa5tq8jw8efmsg",
			"everest16lxq0tq3s4l734j6vc49mvy529ntuflqsxwk0t",
			"everest1dxjwrm4pt5a5z5d37zfpms5tle93net7gjdqwh",
			"everest1n79250d2z7yna0tev9wvjntkfzl7kdwz6sar3u",
			"everest1x6l36he46hk87zlmgfcy955870992yyud0wkr2",
			"everest1cszqlxlrwcmjfjveyv4zah9crfa667fqsx9jse",
			"everest16pjhy7kvf594yca3x4a2cps2rj89vykmz9rhvf",
			"everest1wh5mu68zjwztefgywd2a5axn50fyfcl8hxpfp2",
			"everest1yukenswz8qz3mz4sey6hc50xzyur7gwgcsx9up",
			"everest1z5hdqwewf6gz6erqe2zpchn4drqvch2fcakryy",
			"everest1nnm27zu0njsufg882dlgjkmd7f04jy7pv6d7fw",
			"everest1pms76z8pslchjsx8ham8wz58e77yd9me0hh728",
			"everest1txa0eycfkyx47dqmv564amr382wwk9y85q4rxv",
			"everest1gmm3mtufvu04zmfndftl5par4zkva66g8swlcg",
			"everest1z3g63cuqy57v2jw9zrrcjs05tq33r39d44lxux",
			"everest1ce6xglqcnnrn84qhayfm75eur9s4m4404m7c3y",
			"everest1e9vz05qjfqut0wtvr4jwns5x2stkzhazfla84l",
			"everest1f00w9grssez3nv9w280sqy7hnk3eu49rvxuevw",
			"everest137rave5r7xlfexhlecsx6xslelnsr77x4p9u40",
			"everest1n7tzmmy7px80kaynsch598e0k8kpc9kktvnug5",
			"everest1jwzme4a5972jz77uy40vvhwk0v5pg44psv5swg",
			"everest1ttq2g3w4pa9jf5vcm9lazrlldsqkjlglwhspxa",
			"everest1980ckg4w3p39s35zky0j3sr0m7xhcxh5m3kcr9",
			"everest152c3ufanf4zdlg735gcd9k32ycvfc7edwcq443",
			"everest1wdld6zv0gvxrjhe0jf3vlh8qyqxr7msz9za874",
			"everest1mlkt4exvj0f4fmqsf6nf63sx6u5m2fvrkc26hm",
			"everest1hdcuunv4xyadd8udvna79eq5e5uy8c4ty24jem",
			"everest1hc8ff8m67rzar0eqx9d6xnv6wl3fr66d6n4e7m",
			"everest1gyf5scjflqm67uw3h58mru66symtk33vpvcryq",
			"everest1c6vtdvdvtuxpllql80mfasn7rfj8dtuaygw2kn",
			"everest1m4l75cnyz7mmmcnk8t280fcauz9sux6fra23r5",
			"everest1nvl0x5r0kvn96ez80aqetkqvmavug45ecz2qv4",
			"everest1jhz0runhkyph8txy2227zcahszcyrwfguulh5r",
			"everest1a23shpjrf5l02q3n088nxxkxlmj2gqln6yaz7g",
			"everest1p5ulzqjqstph7tk47rdjq8cfdf3k2f4l4z2d72",
			"everest1nhypt6cxj6genlze8pswuf8v7gx2sy2khhxw8e",
			"everest1hwcus87dp3ucp6sd67jl0vcv9tearvcg3egwdc",
			"everest1n2gdqj5v20gcucg9c2khtxpx4rku5nvfvx5g4j",
			"everest1ak6c62lq023g3z3pcpascp4g6n3cls6km6trx3",
			"everest1szhj9fd5xm457wpw288t0ys70ergm9twfpg2u3",
			"everest1cpryzmqcfct2afcpaswuz0dm72kpveysf8f3lm",
			"everest1dur09g3un57ry0wrk65cp8cgqzy8dudpnez4dd",
			"everest1xll6v7jmm3l4h4q98z2mglrhv54pnd9jecgwuk",
			"everest1hv29fttsv4kdheu94e3x68we0j2mvzra99eyz0",
			"everest1vh4rhzxh3edqn2mhmslehnkqhgzf22r0tc83af",
			"everest14lc7t8fv5htn59mx2evyedxy0036l9lm5vszf5",
			"everest1ma053nws2xf5frxzcpx8dnzs9j3cvxtm7lmhse",
			"everest17e8req3kqu0ndgsrru0afw0afz0relguztdgnk",
			"everest1jgk0k5zct6pfaskten2fcm9n7k5zm3ym76v52w",
			"everest18lmuqusw8ngu5e8kfa8pn9fjrgj97henrael0u",
			"everest1hcg8j4azwdww7a4a2klqse3xmuka09lcz87aml",
			"everest1zvv6ycru9d9uwaznes3zc77e2389wvf5lqjzg5",
			"everest1acxacuu7770p5pez39yzrrgerumuc8ptjyx6vs",
			"everest1s5xxq6d5kqth3z9z9j4a7dplademfg8fdvxrp3",
			"everest179ajjus5gqqq9e25mldqcel9lypdy20lcct9d5",
			"everest16jnuwtzg5fgmpmxvwzmymyk5ch0wpxsp6y7wvr",
			"everest1zx64d2wln5ewvtpe0vadxdpzvpgf57qwfn8357",
			"everest1m40dskpwh79dkhglt4knzhwsx4zywf2qdq22qt",
		},
		StakerIDs: []string{
			"NodeID-NpagUxt6KQiwPch9Sd4osv8kD1TZnkjdk",
			"NodeID-2m38qc95mhHXtrhjyGbe7r2NhniqHHJRB",
			"NodeID-LQwRLm4cbJ7T2kxcxp4uXCU5XD8DFrE1C",
			"NodeID-hArafGhY2HFTbwaaVh1CSCUCUCiJ2Vfb",
			"NodeID-4QBwET5o8kUhvt9xArhir4d3R25CtmZho",
			"NodeID-HGZ8ae74J3odT8ESreAdCtdnvWG1J4X5n",
			"NodeID-4KXitMCoE9p2BHA6VzXtaTxLoEjNDo2Pt",
			"NodeID-JyE4P8f4cTryNV8DCz2M81bMtGhFFHexG",
			"NodeID-EzGaipqomyK9UKx9DBHV6Ky3y68hoknrF",
			"NodeID-CYKruAjwH1BmV3m37sXNuprbr7dGQuJwG",
			"NodeID-LegbVf6qaMKcsXPnLStkdc1JVktmmiDxy",
			"NodeID-FesGqwKq7z5nPFHa5iwZctHE5EZV9Lpdq",
			"NodeID-BFa1padLXBj7VHa2JYvYGzcTBPQGjPhUy",
			"NodeID-4B4rc5vdD1758JSBYL1xyvE5NHGzz6xzH",
			"NodeID-EDESh4DfZFC15i613pMtWniQ9arbBZRnL",
			"NodeID-CZmZ9xpCzkWqjAyS7L4htzh5Lg6kf1k18",
			"NodeID-CTtkcXvVdhpNp6f97LEUXPwsRD3A2ZHqP",
			"NodeID-84KbQHSDnojroCVY7vQ7u9Tx7pUonPaS",
			"NodeID-JjvzhxnLHLUQ5HjVRkvG827ivbLXPwA9u",
			"NodeID-4CWTbdvgXHY1CLXqQNAp22nJDo5nAmts6",
		},
		EVMBytes: evmGenesisBytes,
		Message:  "Now I am become Death, the destroyer of worlds.",
	}
	DenaliConfig = Config{
		NetworkID: constants.DenaliID,
		MintAddresses: []string{
			"denali1tzw0pxhlerlpu7ajws86cv6tzmm5lsaz47zfcs",
		},
		FundedAddresses: []string{
			"denali1vxn8qxc4wjaumd5zefh40vjx2npjeysmcmp58k",
			"denali1hcak2mtd758w0jdty76svreu84kj59fxaw96vx",
			"denali1gmf872653xykm8j7et2k8mxld046arrjslw3ep",
			"denali1gvvymfqhwns6zpy2fwnjwmfl2hkj9k4zys3sda",
			"denali19p2cct7hyz2yyh3pewltgkdr6yj67vfzehlys9",
			"denali10snn22afww6pkek89xpxu9varewxe26dgmyen9",
			"denali1y2j34gqw4nc459cmluq560xxms3l8ek8px4h2g",
			"denali13j2sd0dfngaznc5e4dzk02yhp4g3et6svhf32v",
			"denali1qc4ly2zz6zq95sd7r9r53q8hzywj0j55d6wpqk",
			"denali1qy8awey258pffs4xxl7pjs6mm8yvhhw9haqx7k",
		},
		StakerIDs: []string{
			"NodeID-LQwRLm4cbJ7T2kxcxp4uXCU5XD8DFrE1C",
			"NodeID-hArafGhY2HFTbwaaVh1CSCUCUCiJ2Vfb",
			"NodeID-2m38qc95mhHXtrhjyGbe7r2NhniqHHJRB",
			"NodeID-4QBwET5o8kUhvt9xArhir4d3R25CtmZho",
			"NodeID-NpagUxt6KQiwPch9Sd4osv8kD1TZnkjdk",
		},
		EVMBytes: evmGenesisBytes,
	}
	CascadeConfig = Config{
		NetworkID: constants.CascadeID,
		MintAddresses: []string{
			"cascade1tzw0pxhlerlpu7ajws86cv6tzmm5lsazrac34s",
		},
		FundedAddresses: []string{
			"cascade1vxn8qxc4wjaumd5zefh40vjx2npjeysmwcmv2k",
			"cascade1hcak2mtd758w0jdty76svreu84kj59fxtdlzpx",
			"cascade1gmf872653xykm8j7et2k8mxld046arrjxu5f5p",
			"cascade1gvvymfqhwns6zpy2fwnjwmfl2hkj9k4zjntgqa",
			"cascade19p2cct7hyz2yyh3pewltgkdr6yj67vfz059ua9",
			"cascade10snn22afww6pkek89xpxu9varewxe26d7c7p79",
			"cascade1y2j34gqw4nc459cmluq560xxms3l8ek8h9008g",
			"cascade13j2sd0dfngaznc5e4dzk02yhp4g3et6s65nf8v",
			"cascade1qc4ly2zz6zq95sd7r9r53q8hzywj0j55me5edk",
			"cascade1qy8awey258pffs4xxl7pjs6mm8yvhhw9p767nk",
		},
		StakerIDs: []string{
			"NodeID-NX4zVkuiRJZYe6Nzzav7GXN3TakUet3Co",
			"NodeID-CMsa8cMw4eib1Hb8GG4xiUKAq5eE1BwUX",
			"NodeID-DsMP6jLhi1MkDVc3qx9xx9AAZWx8e87Jd",
			"NodeID-N86eodVZja3GEyZJTo3DFUPGpxEEvjGHs",
			"NodeID-EkKeGSLUbHrrtuayBtbwgWDRUiAziC3ao",
		},
		EVMBytes: evmGenesisBytes,
	}
	LocalConfig = Config{
		NetworkID:     constants.LocalID,
		MintAddresses: []string{},
		FundedAddresses: []string{
			// Private key: ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN
			"local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
		},
		StakerIDs: []string{
			"NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg",
			"NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ",
			"NodeID-NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN",
			"NodeID-GWPcbFJZFfZreETSoWjPimr846mXEKCtu",
			"NodeID-P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5",
		},
		EVMBytes: localEVMGenesisBytes,
		Message:  "Hello World!",
	}
	CustomConfig = Config{
		NetworkID:     math.MaxUint32,
		MintAddresses: []string{},
		FundedAddresses: []string{
			// Private key: ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN
			"custom18jma8ppw3nhx5r4ap8clazz0dps7rv5u9xde7p",
		},
		StakerIDs: []string{
			"NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg",
			"NodeID-MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ",
			"NodeID-NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN",
			"NodeID-GWPcbFJZFfZreETSoWjPimr846mXEKCtu",
			"NodeID-P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5",
		},
		EVMBytes: localEVMGenesisBytes,
	}
)

// GetConfig ...
func GetConfig(networkID uint32) *Config {
	switch networkID {
	case constants.EverestID:
		return &EverestConfig
	case constants.DenaliID:
		return &DenaliConfig
	case constants.CascadeID:
		return &CascadeConfig
	case constants.LocalID:
		return &LocalConfig
	default:
		return &CustomConfig
	}
}
