// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/stretchr/testify/require"
)

func TestPAddressConversion(t *testing.T) {
	tests := []struct {
		pubKey  string
		address string
	}{
		{
			"0x0405e08a3bc814a34203ed633e006433db032bccd052900c5030a9b1ea93e9c985e7348e3b9efe1f537895ebde927e17cc37fffa35c368cb9d1409a20f32c1ab8b",
			"X-columbus18l0l8dz03vmk7k7y5cgax8k2l5mphj4p73ph4p",
		},
		{
			"0x02df1dae869b405c3e5887f9a5239e7f01219951d0bafc7b3f54711031c2c71942",
			"X-kopernikus1uuudvyuqef9wacp8shquts36tj0z2hskdj2ydl",
		},
		{
			"04520df316eb6a27493510c2fcf18235f11183203ca1532cba77978953238993b56af8b3ddc04b3462a0d2c8292fda14f98ab580c11e365a0ffec0ac3969a506b3",
			"P-columbus104903g9rdv97twue5g79a8shkyh8sd4nzx0wjn",
		},
		{
			"0x04554b5f82b5683536b5a125b2ac7c4a90dd1b8cf2e39d5fc723a5bed1ee9aaafb6741eeee4ef8a3210d9bfe0ac82631d72eb8b96ed8d81727e4508e4bb7f451a8",
			"P-columbus15edgkc7d8ptxy9kvl9x66a0hapu7xummkpu9cd",
		},
		{
			"049cfbac90749c7261306e2c2209653027992166fb720879d8c18b8bcf1ec75ea4e289fb8646c5e711f45af9eac761b19ceec02de608a9d6b9fcf91aafc60e5d57",
			"P-columbus1k3l0wtdstm8fse5wenuwrqv2x92pu94x0wrpg4",
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%s->%s", test.pubKey[:7], test.address), func(t *testing.T) {
			pk, err := PublicKeyFromString(test.pubKey)
			require.NoError(t, err)

			testAddr, err := ToPAddress(pk)
			require.NoError(t, err)

			assertPAaddress(t, test.address, testAddr)
		})
	}
}

func TestPAddressFormatting(t *testing.T) {
	tests := []struct {
		pubKey  string
		address string
	}{
		{
			"0x0405e08a3bc814a34203ed633e006433db032bccd052900c5030a9b1ea93e9c985e7348e3b9efe1f537895ebde927e17cc37fffa35c368cb9d1409a20f32c1ab8b",
			"X-columbus18l0l8dz03vmk7k7y5cgax8k2l5mphj4p73ph4p",
		},
		{
			"0x02df1dae869b405c3e5887f9a5239e7f01219951d0bafc7b3f54711031c2c71942",
			"X-kopernikus1uuudvyuqef9wacp8shquts36tj0z2hskdj2ydl",
		},
		{
			"04520df316eb6a27493510c2fcf18235f11183203ca1532cba77978953238993b56af8b3ddc04b3462a0d2c8292fda14f98ab580c11e365a0ffec0ac3969a506b3",
			"P-columbus104903g9rdv97twue5g79a8shkyh8sd4nzx0wjn",
		},
		{
			"0x04554b5f82b5683536b5a125b2ac7c4a90dd1b8cf2e39d5fc723a5bed1ee9aaafb6741eeee4ef8a3210d9bfe0ac82631d72eb8b96ed8d81727e4508e4bb7f451a8",
			"P-columbus15edgkc7d8ptxy9kvl9x66a0hapu7xummkpu9cd",
		},
		{
			"049cfbac90749c7261306e2c2209653027992166fb720879d8c18b8bcf1ec75ea4e289fb8646c5e711f45af9eac761b19ceec02de608a9d6b9fcf91aafc60e5d57\n",
			"P-columbus1k3l0wtdstm8fse5wenuwrqv2x92pu94x0wrpg4",
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%s->%s", test.pubKey[:7], test.address), func(t *testing.T) {
			cID, hRP, _, err := address.Parse(test.address)
			require.NoError(t, err, "wrong test case address %s", test.address)

			pk, err := PublicKeyFromString(test.pubKey)
			require.NoError(t, err)

			testAddr, err := ToPAddress(pk)
			require.NoError(t, err)
			formattedAddr, err := address.Format(cID, hRP, testAddr.Bytes())
			require.NoError(t, err)

			require.Equal(t, test.address, formattedAddr)
		})
	}
}

func TestCAddressConversion(t *testing.T) {
	tests := []struct {
		pubKey  string
		address string
	}{
		{
			"0x0405e08a3bc814a34203ed633e006433db032bccd052900c5030a9b1ea93e9c985e7348e3b9efe1f537895ebde927e17cc37fffa35c368cb9d1409a20f32c1ab8b",
			"0xf0666140A161e725e8D2cB4C445a06F7b8886f11",
		},
		{
			"0x02df1dae869b405c3e5887f9a5239e7f01219951d0bafc7b3f54711031c2c71942",
			"0x09ed81afa4c54edbd24aff2a580a96e03bc40c84",
		},
		{
			"04520df316eb6a27493510c2fcf18235f11183203ca1532cba77978953238993b56af8b3ddc04b3462a0d2c8292fda14f98ab580c11e365a0ffec0ac3969a506b3",
			"0x8633862c9ac77fa94384532802ff1024323a8f79",
		},
		{
			"0x04554b5f82b5683536b5a125b2ac7c4a90dd1b8cf2e39d5fc723a5bed1ee9aaafb6741eeee4ef8a3210d9bfe0ac82631d72eb8b96ed8d81727e4508e4bb7f451a8",
			"cfed1a1baa164134a8e43100250363fa763f53b3",
		},
		{
			"049cfbac90749c7261306e2c2209653027992166fb720879d8c18b8bcf1ec75ea4e289fb8646c5e711f45af9eac761b19ceec02de608a9d6b9fcf91aafc60e5d57",
			"990bf4bda7d446c7513b7c00744a287e8328011c",
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%s->%s", test.pubKey[:7], test.address), func(t *testing.T) {
			pk, err := PublicKeyFromString(test.pubKey)
			require.NoError(t, err)

			testAddr, err := ToCAddress(pk)
			require.NoError(t, err)

			assertCAaddress(t, test.address, testAddr)
		})
	}
}

func assertCAaddress(t *testing.T, expected string, calc ids.ShortID) {
	addrBytes, err := hex.DecodeString(strings.TrimPrefix(expected, "0x"))
	require.NoError(t, err, "wrong test case address %s", expected)
	expectedAddr, err := ids.ToShortID(addrBytes)
	require.NoError(t, err, "test case address %s cannot be converted", expected)

	require.Equal(t, expectedAddr, calc, "expected %s, got %s", expected, calc)
}

func assertPAaddress(t *testing.T, expected string, calc ids.ShortID) {
	_, _, addrBytes, err := address.Parse(expected)
	require.NoError(t, err, "wrong test case address %s", expected)
	expectedAddr, err := ids.ToShortID(addrBytes)
	require.NoError(t, err, "test case address %s cannot be converted", expected)

	require.Equal(t, expectedAddr, calc, "expected %s, got %s", expected, calc)
}
