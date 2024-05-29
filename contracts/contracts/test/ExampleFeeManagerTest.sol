//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;
pragma experimental ABIEncoderV2;

import "../ExampleFeeManager.sol";
import "./AllowListTest.sol";
import "../AllowList.sol";
import "../interfaces/IFeeManager.sol";

contract ExampleFeeManagerTest is AllowListTest {
  IFeeManager manager = IFeeManager(FEE_MANAGER_ADDRESS);

  uint256 testNumber;

  function setUp() public {
    // noop
  }

  function step_addContractDeployerAsOwner() public {
    ExampleFeeManager example = new ExampleFeeManager();
    assertEq(address(this), example.owner());
  }

  function step_enableWAGMIFeesFailure() public {
    ExampleFeeManager example = new ExampleFeeManager();

    assertRole(manager.readAllowList(address(this)), AllowList.Role.Admin);
    assertRole(manager.readAllowList(address(example)), AllowList.Role.None);

    try example.enableWAGMIFees() {
      assertTrue(false, "enableWAGMIFees should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected
  }

  function step_addContractToManagerList() public {
    ExampleFeeManager example = new ExampleFeeManager();

    address exampleAddress = address(example);
    address thisAddress = address(this);

    assertRole(manager.readAllowList(thisAddress), AllowList.Role.Admin);
    assertRole(manager.readAllowList(exampleAddress), AllowList.Role.None);

    manager.setEnabled(exampleAddress);

    assertRole(manager.readAllowList(exampleAddress), AllowList.Role.Enabled);
  }

  function step_changeFees() public {
    ExampleFeeManager example = new ExampleFeeManager();
    address exampleAddress = address(example);

    manager.setEnabled(exampleAddress);

    FeeConfig memory config = example.getCurrentFeeConfig();

    FeeConfig memory newFeeConfig = FeeConfig({
      gasLimit: CCHAIN_GAS_LIMIT,
      targetBlockRate: CCHAIN_TARGET_BLOCK_RATE,
      minBaseFee: CCHAIN_MIN_BASE_FEE,
      targetGas: CCHAIN_TARGET_GAS,
      baseFeeChangeDenominator: CCHAIN_BASE_FEE_CHANGE_DENOMINATOR,
      minBlockGasCost: CCHAIN_MIN_BLOCK_GAS_COST,
      maxBlockGasCost: CCHAIN_MAX_BLOCK_GAS_COST,
      blockGasCostStep: CCHAIN_BLOCK_GAS_COST_STEP
    });

    assertNotEq(config.gasLimit, newFeeConfig.gasLimit);
    // target block rate is the same for wagmi and cchain
    // assertNotEq(config.targetBlockRate, newFeeConfig.targetBlockRate);
    assertNotEq(config.minBaseFee, newFeeConfig.minBaseFee);
    assertNotEq(config.targetGas, newFeeConfig.targetGas);
    assertNotEq(config.baseFeeChangeDenominator, newFeeConfig.baseFeeChangeDenominator);
    // min block gas cost is the same for wagmi and cchain
    // assertNotEq(config.minBlockGasCost, newFeeConfig.minBlockGasCost);
    assertNotEq(config.maxBlockGasCost, newFeeConfig.maxBlockGasCost);
    assertNotEq(config.blockGasCostStep, newFeeConfig.blockGasCostStep);

    example.enableCChainFees();

    FeeConfig memory changedFeeConfig = example.getCurrentFeeConfig();

    assertEq(changedFeeConfig.gasLimit, newFeeConfig.gasLimit);
    // target block rate is the same for wagmi and cchain
    // assertEq(changedFeeConfig.targetBlockRate, newFeeConfig.targetBlockRate);
    assertEq(changedFeeConfig.minBaseFee, newFeeConfig.minBaseFee);
    assertEq(changedFeeConfig.targetGas, newFeeConfig.targetGas);
    assertEq(changedFeeConfig.baseFeeChangeDenominator, newFeeConfig.baseFeeChangeDenominator);
    // min block gas cost is the same for wagmi and cchain
    // assertEq(changedFeeConfig.minBlockGasCost, newFeeConfig.minBlockGasCost);
    assertEq(changedFeeConfig.maxBlockGasCost, newFeeConfig.maxBlockGasCost);
    assertEq(changedFeeConfig.blockGasCostStep, newFeeConfig.blockGasCostStep);

    assertEq(example.getFeeConfigLastChangedAt(), block.number);

    // reset fees to what they were before
    example.enableCustomFees(config);
  }

  function step_minFeeTransaction() public {
    // used as a noop for testing min-fees associated with a transaction
  }

  function step_raiseMinFeeByOne() public {
    ExampleFeeManager example = new ExampleFeeManager();
    address exampleAddress = address(example);

    manager.setEnabled(exampleAddress);

    FeeConfig memory config = example.getCurrentFeeConfig();
    config.minBaseFee = config.minBaseFee + 1;

    example.enableCustomFees(config);
  }

  function step_lowerMinFeeByOne() public {
    ExampleFeeManager example = new ExampleFeeManager();
    address exampleAddress = address(example);

    manager.setEnabled(exampleAddress);

    FeeConfig memory config = example.getCurrentFeeConfig();
    config.minBaseFee = config.minBaseFee - 1;

    example.enableCustomFees(config);
  }
}
