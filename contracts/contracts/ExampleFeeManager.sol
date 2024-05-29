//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;
pragma experimental ABIEncoderV2;

import "@openzeppelin/contracts/access/Ownable.sol";
import "./AllowList.sol";
import "./interfaces/IFeeManager.sol";

address constant FEE_MANAGER_ADDRESS = 0x0200000000000000000000000000000000000003;

uint constant WAGMI_GAS_LIMIT = 20_000_000;
uint constant WAGMI_TARGET_BLOCK_RATE = 2;
uint constant WAGMI_MIN_BASE_FEE = 1_000_000_000;
uint constant WAGMI_TARGET_GAS = 100_000_000;
uint constant WAGMI_BASE_FEE_CHANGE_DENOMINATOR = 48;
uint constant WAGMI_MIN_BLOCK_GAS_COST = 0;
uint constant WAGMI_MAX_BLOCK_GAS_COST = 10_000_000;
uint constant WAGMI_BLOCK_GAS_COST_STEP = 500_000;

uint constant CCHAIN_GAS_LIMIT = 8_000_000;
uint constant CCHAIN_TARGET_BLOCK_RATE = 2;
uint constant CCHAIN_MIN_BASE_FEE = 25_000_000_000;
uint constant CCHAIN_TARGET_GAS = 15_000_000;
uint constant CCHAIN_BASE_FEE_CHANGE_DENOMINATOR = 36;
uint constant CCHAIN_MIN_BLOCK_GAS_COST = 0;
uint constant CCHAIN_MAX_BLOCK_GAS_COST = 1_000_000;
uint constant CCHAIN_BLOCK_GAS_COST_STEP = 100_000;

struct FeeConfig {
  uint256 gasLimit;
  uint256 targetBlockRate;
  uint256 minBaseFee;
  uint256 targetGas;
  uint256 baseFeeChangeDenominator;
  uint256 minBlockGasCost;
  uint256 maxBlockGasCost;
  uint256 blockGasCostStep;
}

// ExampleFeeManager shows how FeeManager precompile can be used in a smart contract
// All methods of [allowList] can be directly called. There are example calls as tasks in hardhat.config.ts file.
contract ExampleFeeManager is AllowList {
  IFeeManager feeManager = IFeeManager(FEE_MANAGER_ADDRESS);

  constructor() AllowList(FEE_MANAGER_ADDRESS) {}

  function enableWAGMIFees() public onlyEnabled {
    feeManager.setFeeConfig(
      WAGMI_GAS_LIMIT,
      WAGMI_TARGET_BLOCK_RATE,
      WAGMI_MIN_BASE_FEE,
      WAGMI_TARGET_GAS,
      WAGMI_BASE_FEE_CHANGE_DENOMINATOR,
      WAGMI_MIN_BLOCK_GAS_COST,
      WAGMI_MAX_BLOCK_GAS_COST,
      WAGMI_BLOCK_GAS_COST_STEP
    );
  }

  function enableCChainFees() public onlyEnabled {
    feeManager.setFeeConfig(
      CCHAIN_GAS_LIMIT,
      CCHAIN_TARGET_BLOCK_RATE,
      CCHAIN_MIN_BASE_FEE,
      CCHAIN_TARGET_GAS,
      CCHAIN_BASE_FEE_CHANGE_DENOMINATOR,
      CCHAIN_MIN_BLOCK_GAS_COST,
      CCHAIN_MAX_BLOCK_GAS_COST,
      CCHAIN_BLOCK_GAS_COST_STEP
    );
  }

  function enableCustomFees(FeeConfig memory config) public onlyEnabled {
    feeManager.setFeeConfig(
      config.gasLimit,
      config.targetBlockRate,
      config.minBaseFee,
      config.targetGas,
      config.baseFeeChangeDenominator,
      config.minBlockGasCost,
      config.maxBlockGasCost,
      config.blockGasCostStep
    );
  }

  function getCurrentFeeConfig() public view returns (FeeConfig memory) {
    FeeConfig memory config;
    (
      config.gasLimit,
      config.targetBlockRate,
      config.minBaseFee,
      config.targetGas,
      config.baseFeeChangeDenominator,
      config.minBlockGasCost,
      config.maxBlockGasCost,
      config.blockGasCostStep
    ) = feeManager.getFeeConfig();
    return config;
  }

  function getFeeConfigLastChangedAt() public view returns (uint256) {
    return feeManager.getFeeConfigLastChangedAt();
  }
}
