//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./IFeeManager.sol";

contract FeeManagerTest {
  IFeeManager private feeManager;

  constructor(address feeManagerPrecompile) {
    feeManager = IFeeManager(feeManagerPrecompile);
  }

  // Calls the setFeeConfig function on the precompile
  function setFeeConfig(
    uint256 gasLimit,
    uint256 targetBlockRate,
    uint256 minBaseFee,
    uint256 targetGas,
    uint256 baseFeeChangeDenominator,
    uint256 minBlockGasCost,
    uint256 maxBlockGasCost,
    uint256 blockGasCostStep
  ) external {
    feeManager.setFeeConfig(
      gasLimit,
      targetBlockRate,
      minBaseFee,
      targetGas,
      baseFeeChangeDenominator,
      minBlockGasCost,
      maxBlockGasCost,
      blockGasCostStep
    );
  }

  // Calls the getFeeConfig function on the precompile
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
    )
  {
    return feeManager.getFeeConfig();
  }

  // Calls the getFeeConfigLastChangedAt function on the precompile
  function getFeeConfigLastChangedAt() external view returns (uint256) {
    return feeManager.getFeeConfigLastChangedAt();
  }
}
