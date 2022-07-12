//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;
pragma experimental ABIEncoderV2;

import "@openzeppelin/contracts/access/Ownable.sol";
import "./AllowList.sol";
import "./IFeeManager.sol";

// ExampleFeeManager shows how FeeConfigManager precompile can be used in a smart contract
// All methods of [allowList] can be directly called. There are example calls as tasks in hardhat.config.ts file.
contract ExampleFeeManager is AllowList {
  // Precompiled Fee Manager Contract Address
  address constant FEE_MANAGER_ADDRESS = 0x0200000000000000000000000000000000000003;
  IFeeManager feeManager = IFeeManager(FEE_MANAGER_ADDRESS);

  bytes32 public constant MANAGER_ROLE = keccak256("MANAGER_ROLE");

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

  constructor() AllowList(FEE_MANAGER_ADDRESS) {}

  function enableWAGMIFees() public onlyEnabled {
    feeManager.setFeeConfig(
      20_000_000, // gasLimit
      2, // targetBlockRate
      1_000_000_000, // minBaseFee
      100_000_000, // targetGas
      48, // baseFeeChangeDenominator
      0, // minBlockGasCost
      10_000_000, // maxBlockGasCost
      500_000 // blockGasCostStep
    );
  }

  function enableCChainFees() public onlyEnabled {
    feeManager.setFeeConfig(
      8_000_000, // gasLimit
      2, // targetBlockRate
      25_000_000_000, // minBaseFee
      15_000_000, // targetGas
      36, // baseFeeChangeDenominator
      0, // minBlockGasCost
      1_000_000, // maxBlockGasCost
      200_000 // blockGasCostStep
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
