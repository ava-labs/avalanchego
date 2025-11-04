// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/assert"

	ethparams "github.com/ava-labs/libevm/params"
)

// TestUpstreamParamsValues detects when a params value changes upstream to prevent a subtle change
// to one of the values to have an unpredicted impact in the libevm consumer.
// Values should be updated to newer upstream values once the consumer is updated to handle the
// updated value(s).
func TestUpstreamParamsValues(t *testing.T) {
	tests := map[string]struct {
		param any
		want  any
	}{
		"GasLimitBoundDivisor":               {param: ethparams.GasLimitBoundDivisor, want: uint64(1024)},
		"MinGasLimit":                        {param: ethparams.MinGasLimit, want: uint64(5000)},
		"MaxGasLimit":                        {param: ethparams.MaxGasLimit, want: uint64(0x7fffffffffffffff)},
		"GenesisGasLimit":                    {param: ethparams.GenesisGasLimit, want: uint64(4712388)},
		"ExpByteGas":                         {param: ethparams.ExpByteGas, want: uint64(10)},
		"SloadGas":                           {param: ethparams.SloadGas, want: uint64(50)},
		"CallValueTransferGas":               {param: ethparams.CallValueTransferGas, want: uint64(9000)},
		"CallNewAccountGas":                  {param: ethparams.CallNewAccountGas, want: uint64(25000)},
		"TxGas":                              {param: ethparams.TxGas, want: uint64(21000)},
		"TxGasContractCreation":              {param: ethparams.TxGasContractCreation, want: uint64(53000)},
		"TxDataZeroGas":                      {param: ethparams.TxDataZeroGas, want: uint64(4)},
		"QuadCoeffDiv":                       {param: ethparams.QuadCoeffDiv, want: uint64(512)},
		"LogDataGas":                         {param: ethparams.LogDataGas, want: uint64(8)},
		"CallStipend":                        {param: ethparams.CallStipend, want: uint64(2300)},
		"Keccak256Gas":                       {param: ethparams.Keccak256Gas, want: uint64(30)},
		"Keccak256WordGas":                   {param: ethparams.Keccak256WordGas, want: uint64(6)},
		"InitCodeWordGas":                    {param: ethparams.InitCodeWordGas, want: uint64(2)},
		"SstoreSetGas":                       {param: ethparams.SstoreSetGas, want: uint64(20000)},
		"SstoreResetGas":                     {param: ethparams.SstoreResetGas, want: uint64(5000)},
		"SstoreClearGas":                     {param: ethparams.SstoreClearGas, want: uint64(5000)},
		"SstoreRefundGas":                    {param: ethparams.SstoreRefundGas, want: uint64(15000)},
		"NetSstoreNoopGas":                   {param: ethparams.NetSstoreNoopGas, want: uint64(200)},
		"NetSstoreInitGas":                   {param: ethparams.NetSstoreInitGas, want: uint64(20000)},
		"NetSstoreCleanGas":                  {param: ethparams.NetSstoreCleanGas, want: uint64(5000)},
		"NetSstoreDirtyGas":                  {param: ethparams.NetSstoreDirtyGas, want: uint64(200)},
		"NetSstoreClearRefund":               {param: ethparams.NetSstoreClearRefund, want: uint64(15000)},
		"NetSstoreResetRefund":               {param: ethparams.NetSstoreResetRefund, want: uint64(4800)},
		"NetSstoreResetClearRefund":          {param: ethparams.NetSstoreResetClearRefund, want: uint64(19800)},
		"SstoreSentryGasEIP2200":             {param: ethparams.SstoreSentryGasEIP2200, want: uint64(2300)},
		"SstoreSetGasEIP2200":                {param: ethparams.SstoreSetGasEIP2200, want: uint64(20000)},
		"SstoreResetGasEIP2200":              {param: ethparams.SstoreResetGasEIP2200, want: uint64(5000)},
		"SstoreClearsScheduleRefundEIP2200":  {param: ethparams.SstoreClearsScheduleRefundEIP2200, want: uint64(15000)},
		"ColdAccountAccessCostEIP2929":       {param: ethparams.ColdAccountAccessCostEIP2929, want: uint64(2600)},
		"ColdSloadCostEIP2929":               {param: ethparams.ColdSloadCostEIP2929, want: uint64(2100)},
		"WarmStorageReadCostEIP2929":         {param: ethparams.WarmStorageReadCostEIP2929, want: uint64(100)},
		"SstoreClearsScheduleRefundEIP3529":  {param: ethparams.SstoreClearsScheduleRefundEIP3529, want: uint64(5000 - 2100 + 1900)},
		"JumpdestGas":                        {param: ethparams.JumpdestGas, want: uint64(1)},
		"EpochDuration":                      {param: ethparams.EpochDuration, want: uint64(30000)},
		"CreateDataGas":                      {param: ethparams.CreateDataGas, want: uint64(200)},
		"CallCreateDepth":                    {param: ethparams.CallCreateDepth, want: uint64(1024)},
		"ExpGas":                             {param: ethparams.ExpGas, want: uint64(10)},
		"LogGas":                             {param: ethparams.LogGas, want: uint64(375)},
		"CopyGas":                            {param: ethparams.CopyGas, want: uint64(3)},
		"StackLimit":                         {param: ethparams.StackLimit, want: uint64(1024)},
		"TierStepGas":                        {param: ethparams.TierStepGas, want: uint64(0)},
		"LogTopicGas":                        {param: ethparams.LogTopicGas, want: uint64(375)},
		"CreateGas":                          {param: ethparams.CreateGas, want: uint64(32000)},
		"Create2Gas":                         {param: ethparams.Create2Gas, want: uint64(32000)},
		"SelfdestructRefundGas":              {param: ethparams.SelfdestructRefundGas, want: uint64(24000)},
		"MemoryGas":                          {param: ethparams.MemoryGas, want: uint64(3)},
		"TxDataNonZeroGasFrontier":           {param: ethparams.TxDataNonZeroGasFrontier, want: uint64(68)},
		"TxDataNonZeroGasEIP2028":            {param: ethparams.TxDataNonZeroGasEIP2028, want: uint64(16)},
		"TxAccessListAddressGas":             {param: ethparams.TxAccessListAddressGas, want: uint64(2400)},
		"TxAccessListStorageKeyGas":          {param: ethparams.TxAccessListStorageKeyGas, want: uint64(1900)},
		"CallGasFrontier":                    {param: ethparams.CallGasFrontier, want: uint64(40)},
		"CallGasEIP150":                      {param: ethparams.CallGasEIP150, want: uint64(700)},
		"BalanceGasFrontier":                 {param: ethparams.BalanceGasFrontier, want: uint64(20)},
		"BalanceGasEIP150":                   {param: ethparams.BalanceGasEIP150, want: uint64(400)},
		"BalanceGasEIP1884":                  {param: ethparams.BalanceGasEIP1884, want: uint64(700)},
		"ExtcodeSizeGasFrontier":             {param: ethparams.ExtcodeSizeGasFrontier, want: uint64(20)},
		"ExtcodeSizeGasEIP150":               {param: ethparams.ExtcodeSizeGasEIP150, want: uint64(700)},
		"SloadGasFrontier":                   {param: ethparams.SloadGasFrontier, want: uint64(50)},
		"SloadGasEIP150":                     {param: ethparams.SloadGasEIP150, want: uint64(200)},
		"SloadGasEIP1884":                    {param: ethparams.SloadGasEIP1884, want: uint64(800)},
		"SloadGasEIP2200":                    {param: ethparams.SloadGasEIP2200, want: uint64(800)},
		"ExtcodeHashGasConstantinople":       {param: ethparams.ExtcodeHashGasConstantinople, want: uint64(400)},
		"ExtcodeHashGasEIP1884":              {param: ethparams.ExtcodeHashGasEIP1884, want: uint64(700)},
		"SelfdestructGasEIP150":              {param: ethparams.SelfdestructGasEIP150, want: uint64(5000)},
		"ExpByteFrontier":                    {param: ethparams.ExpByteFrontier, want: uint64(10)},
		"ExpByteEIP158":                      {param: ethparams.ExpByteEIP158, want: uint64(50)},
		"ExtcodeCopyBaseFrontier":            {param: ethparams.ExtcodeCopyBaseFrontier, want: uint64(20)},
		"ExtcodeCopyBaseEIP150":              {param: ethparams.ExtcodeCopyBaseEIP150, want: uint64(700)},
		"CreateBySelfdestructGas":            {param: ethparams.CreateBySelfdestructGas, want: uint64(25000)},
		"DefaultBaseFeeChangeDenominator":    {param: ethparams.DefaultBaseFeeChangeDenominator, want: 8},
		"DefaultElasticityMultiplier":        {param: ethparams.DefaultElasticityMultiplier, want: 2},
		"InitialBaseFee":                     {param: ethparams.InitialBaseFee, want: 1000000000},
		"MaxCodeSize":                        {param: ethparams.MaxCodeSize, want: 24576},
		"MaxInitCodeSize":                    {param: ethparams.MaxInitCodeSize, want: 2 * 24576},
		"EcrecoverGas":                       {param: ethparams.EcrecoverGas, want: uint64(3000)},
		"Sha256BaseGas":                      {param: ethparams.Sha256BaseGas, want: uint64(60)},
		"Sha256PerWordGas":                   {param: ethparams.Sha256PerWordGas, want: uint64(12)},
		"Ripemd160BaseGas":                   {param: ethparams.Ripemd160BaseGas, want: uint64(600)},
		"Ripemd160PerWordGas":                {param: ethparams.Ripemd160PerWordGas, want: uint64(120)},
		"IdentityBaseGas":                    {param: ethparams.IdentityBaseGas, want: uint64(15)},
		"IdentityPerWordGas":                 {param: ethparams.IdentityPerWordGas, want: uint64(3)},
		"Bn256AddGasByzantium":               {param: ethparams.Bn256AddGasByzantium, want: uint64(500)},
		"Bn256AddGasIstanbul":                {param: ethparams.Bn256AddGasIstanbul, want: uint64(150)},
		"Bn256ScalarMulGasByzantium":         {param: ethparams.Bn256ScalarMulGasByzantium, want: uint64(40000)},
		"Bn256ScalarMulGasIstanbul":          {param: ethparams.Bn256ScalarMulGasIstanbul, want: uint64(6000)},
		"Bn256PairingBaseGasByzantium":       {param: ethparams.Bn256PairingBaseGasByzantium, want: uint64(100000)},
		"Bn256PairingBaseGasIstanbul":        {param: ethparams.Bn256PairingBaseGasIstanbul, want: uint64(45000)},
		"Bn256PairingPerPointGasByzantium":   {param: ethparams.Bn256PairingPerPointGasByzantium, want: uint64(80000)},
		"Bn256PairingPerPointGasIstanbul":    {param: ethparams.Bn256PairingPerPointGasIstanbul, want: uint64(34000)},
		"Bls12381G1AddGas":                   {param: ethparams.Bls12381G1AddGas, want: uint64(600)},
		"Bls12381G1MulGas":                   {param: ethparams.Bls12381G1MulGas, want: uint64(12000)},
		"Bls12381G2AddGas":                   {param: ethparams.Bls12381G2AddGas, want: uint64(4500)},
		"Bls12381G2MulGas":                   {param: ethparams.Bls12381G2MulGas, want: uint64(55000)},
		"Bls12381PairingBaseGas":             {param: ethparams.Bls12381PairingBaseGas, want: uint64(115000)},
		"Bls12381PairingPerPairGas":          {param: ethparams.Bls12381PairingPerPairGas, want: uint64(23000)},
		"Bls12381MapG1Gas":                   {param: ethparams.Bls12381MapG1Gas, want: uint64(5500)},
		"Bls12381MapG2Gas":                   {param: ethparams.Bls12381MapG2Gas, want: uint64(110000)},
		"RefundQuotient":                     {param: ethparams.RefundQuotient, want: uint64(2)},
		"RefundQuotientEIP3529":              {param: ethparams.RefundQuotientEIP3529, want: uint64(5)},
		"BlobTxBytesPerFieldElement":         {param: ethparams.BlobTxBytesPerFieldElement, want: 32},
		"BlobTxFieldElementsPerBlob":         {param: ethparams.BlobTxFieldElementsPerBlob, want: 4096},
		"BlobTxBlobGasPerBlob":               {param: ethparams.BlobTxBlobGasPerBlob, want: 1 << 17},
		"BlobTxMinBlobGasprice":              {param: ethparams.BlobTxMinBlobGasprice, want: 1},
		"BlobTxBlobGaspriceUpdateFraction":   {param: ethparams.BlobTxBlobGaspriceUpdateFraction, want: 3338477},
		"BlobTxPointEvaluationPrecompileGas": {param: ethparams.BlobTxPointEvaluationPrecompileGas, want: 50000},
		"BlobTxTargetBlobGasPerBlock":        {param: ethparams.BlobTxTargetBlobGasPerBlock, want: 3 * 131072},
		"MaxBlobGasPerBlock":                 {param: ethparams.MaxBlobGasPerBlock, want: 6 * 131072},
		"GenesisDifficulty":                  {param: ethparams.GenesisDifficulty.Int64(), want: int64(131072)},
		"BeaconRootsStorageAddress":          {param: ethparams.BeaconRootsStorageAddress, want: common.HexToAddress("0x000F3df6D732807Ef1319fB7B8bB8522d0Beac02")},
		"SystemAddress":                      {param: ethparams.SystemAddress, want: common.HexToAddress("0xfffffffffffffffffffffffffffffffffffffffe")},
	}

	for name, test := range tests {
		assert.Equal(t, test.want, test.param, name)
	}
}
