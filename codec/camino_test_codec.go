// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package codec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var VersionTests = []func(c GeneralCodec, t testing.TB){
	TestUpgrade,
}

type UpgradeStruct struct {
	UpgradeVersionID UpgradeVersionID
	Field1           uint32 `serialize:"true"`
	Field2           uint32 `serialize:"true"`
	Version1         uint32 `serialize:"true" upgradeVersion:"1"`
	Version2         uint32 `serialize:"true" upgradeVersion:"2"`
}

type UpgradeStructOld struct {
	UpgradeVersionID UpgradeVersionID
	Field1           uint32 `serialize:"true"`
	Field2           uint32 `serialize:"true"`
	Version1         uint32 `serialize:"true" upgradeVersion:"1"`
}

// Test version upgrade. Optional UpgradeVersionID field is only
// marshalled if it matches the UpgradePrefix pattern defined in codec.
// If the struct bytes start with 48 set bits, and if the struct has
// the first member called UpgradeVersionID, it is treated as 48 bit magic
// and 16 bit version (low 16 bits). Refer to codec.BuildUpgradeVersionID()
func TestUpgrade(codec GeneralCodec, t testing.TB) {
	input := UpgradeStruct{
		UpgradeVersionID: 0,
		Field1:           100,
		Field2:           200,
		Version1:         1,
		Version2:         2,
	}

	manager := NewDefaultManager()
	require := require.New(t)

	err := manager.RegisterCodec(0, codec)
	require.NoError(err)

	bytes, err := manager.Marshal(0, &input)
	require.NoError(err)
	// Because UpgradeVersionID doesn't match UpgradeVersionPrefix,
	// UpgradeVersionID will not be marshalled
	require.Equal(10, len(bytes))

	output := UpgradeStruct{}
	_, err = manager.Unmarshal(bytes, &output)
	require.NoError(err)
	require.Equal(input.UpgradeVersionID, output.UpgradeVersionID)
	require.Equal(input.Field1, output.Field1)
	require.Equal(input.Field2, output.Field2)
	require.Equal(uint32(0), output.Version1)

	input.UpgradeVersionID = BuildUpgradeVersionID(1)
	bytes, err = manager.Marshal(0, &input)
	require.NoError(err)
	// Because UpgradeVersionID does match UpgradeVersionPrefix,
	// UpgradeVersionID will be marshalled and upgradeVersion 1 is used
	require.Equal(22, len(bytes))

	output = UpgradeStruct{}
	_, err = manager.Unmarshal(bytes, &output)
	require.NoError(err)
	require.Equal(input.UpgradeVersionID, output.UpgradeVersionID)
	require.Equal(input.Field1, output.Field1)
	require.Equal(input.Field2, output.Field2)
	require.Equal(input.Version1, output.Version1)
	require.Equal(uint32(0), output.Version2)

	input.UpgradeVersionID = BuildUpgradeVersionID(2)
	bytes, err = manager.Marshal(0, &input)
	require.NoError(err)
	// Because UpgradeVersionID does match UpgradeVersionPrefix,
	// UpgradeVersionID will be marshalled and upgradeVersion 2 is used
	require.Equal(26, len(bytes))

	// This should fail because we try to unmarshal version2 but struct only knows version 1
	outputOld := UpgradeStructOld{}
	_, err = manager.Unmarshal(bytes, &outputOld)
	require.ErrorContains(err, "incompatible")
}
