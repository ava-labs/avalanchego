//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;
import "precompile/allowlist/allowlisttest/bindings/IAllowList.sol";

interface IFeeManager is IAllowList {
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
  event FeeConfigChanged(address indexed sender, FeeConfig oldFeeConfig, FeeConfig newFeeConfig);

  // Set fee config fields to contract storage
  function setFeeConfig(
    uint256 gasLimit,
    uint256 targetBlockRate,
    uint256 minBaseFee,
    uint256 targetGas,
    uint256 baseFeeChangeDenominator,
    uint256 minBlockGasCost,
    uint256 maxBlockGasCost,
    uint256 blockGasCostStep
  ) external;

  // Get fee config from the contract storage
  function getFeeConfig()
    external
    view
    returns (
      uint256 gasLimit,
      uint256 targetBlockRate,
      uint256 minBaseFee,
      uint256 targetGas,
      uint256 baseFeeChangeDenominator,
      uint256 minBlockGasCost,
      uint256 maxBlockGasCost,
      uint256 blockGasCostStep
    );

  // Get the last block number changed the fee config from the contract storage
  function getFeeConfigLastChangedAt() external view returns (uint256 blockNumber);
}
