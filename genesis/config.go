// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/evm"
)

// Note that since an AVA network has exactly one Platform Chain,
// and the Platform Chain defines the genesis state of the network
// (who is staking, which chains exist, etc.), defining the genesis
// state of the Platform Chain is the same as defining the genesis
// state of the network.

// Config contains the genesis addresses used to construct a genesis
type Config struct {
	MintAddresses, FundedAddresses, FundedEVMAddresses, StakerIDs []string
	ParsedMintAddresses, ParsedFundedAddresses, ParsedStakerIDs   []ids.ShortID
}

func (c *Config) init() error {
	c.ParsedMintAddresses = nil
	for _, addrStr := range c.MintAddresses {
		addr, err := ids.ShortFromString(addrStr)
		if err != nil {
			return err
		}
		c.ParsedMintAddresses = append(c.ParsedMintAddresses, addr)
	}
	c.ParsedFundedAddresses = nil
	for _, addrStr := range c.FundedAddresses {
		addr, err := ids.ShortFromString(addrStr)
		if err != nil {
			return err
		}
		c.ParsedFundedAddresses = append(c.ParsedFundedAddresses, addr)
	}
	c.ParsedStakerIDs = nil
	for _, addrStr := range c.StakerIDs {
		addr, err := ids.ShortFromString(addrStr)
		if err != nil {
			return err
		}
		c.ParsedStakerIDs = append(c.ParsedStakerIDs, addr)
	}
	return nil
}

// Hard coded genesis constants
var (
	CascadeConfig = Config{
		MintAddresses: []string{
			"95YUFjhDG892VePMzpwKF9JzewGKvGRi3",
		},
		FundedAddresses: []string{
			"9uKvvA7E35QCwLvAaohXTCfFejbf3Rv17",
			"JLrYNMYXANGj43BfWXBxMMAEenUBp1Sbn",
			"7TUTzwrU6nbZtWHjTHEpdneUvjKBxb3EM",
			"77mPUXBdQKwQpPoX6rckCZGLGGdkuG1G6",
			"4gGWdFZ4Gax1B466YKXyKRRpWLb42Afdt",
			"CKTkzAPsRxCreyiDTnjGxLmjMarxF28fi",
			"4ABm9gFHVtsNdcKSd1xsacFkGneSgzpaa",
			"DpL8PTsrjtLzv5J8LL3D2A6YcnCTqrNH9",
			"ZdhZv6oZrmXLyFDy6ovXAu6VxmbTsT2h",
			"6cesTteH62Y5mLoDBUASaBvCXuL2AthL",
		},
		FundedEVMAddresses: []string{
			"0x572f4D80f10f663B5049F789546f25f70Bb62a7F",
		},
		StakerIDs: []string{
			"NX4zVkuiRJZYe6Nzzav7GXN3TakUet3Co",
			"CMsa8cMw4eib1Hb8GG4xiUKAq5eE1BwUX",
			"DsMP6jLhi1MkDVc3qx9xx9AAZWx8e87Jd",
			"N86eodVZja3GEyZJTo3DFUPGpxEEvjGHs",
			"EkKeGSLUbHrrtuayBtbwgWDRUiAziC3ao",
		},
	}
	DefaultConfig = Config{
		MintAddresses: []string{},
		FundedAddresses: []string{
			// Private key: ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN
			"6Y3kysjF9jnHnYkdS9yGAuoHyae2eNmeV",
		},
		FundedEVMAddresses: []string{
			// Private key: evm.GenesisTestKey
			evm.GenesisTestAddr,
		},
		StakerIDs: []string{
			"7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg",
			"MFrZFVCXPv5iCn6M9K6XduxGTYp891xXZ",
			"NFBbbJ4qCmNaCzeW7sxErhvWqvEQMnYcN",
			"GWPcbFJZFfZreETSoWjPimr846mXEKCtu",
			"P7oB2McjBGgW2NXXWVYjV8JEDFoW9xDE5",
		},
	}
)

// GetConfig ...
func GetConfig(networkID uint32) *Config {
	switch networkID {
	case CascadeID:
		return &CascadeConfig
	default:
		return &DefaultConfig
	}
}
