//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../ExampleRewardManager.sol";
import "../interfaces/IRewardManager.sol";
import "./AllowListTest.sol";

address constant BLACKHOLE_ADDRESS = 0x0100000000000000000000000000000000000000;

contract ExampleRewardManagerTest is AllowListTest {
  IRewardManager rewardManager = IRewardManager(REWARD_MANAGER_ADDRESS);

  ExampleRewardManager exampleReceiveFees;
  uint exampleBalance;

  uint blackholeBalance;

  function setUp() public {
    blackholeBalance = BLACKHOLE_ADDRESS.balance;
  }

  function step_captureBlackholeBalance() public {
    blackholeBalance = BLACKHOLE_ADDRESS.balance;
  }

  function step_checkSendFeesToBlackhole() public {
    assertGt(BLACKHOLE_ADDRESS.balance, blackholeBalance);
  }

  function step_doesNotSetRewardAddressBeforeEnabled() public {
    ExampleRewardManager example = new ExampleRewardManager();
    address exampleAddress = address(example);

    assertRole(rewardManager.readAllowList(exampleAddress), AllowList.Role.None);

    try example.setRewardAddress(exampleAddress) {
      assertTrue(false, "setRewardAddress should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected
  }

  function step_setEnabled() public {
    ExampleRewardManager example = new ExampleRewardManager();
    address exampleAddress = address(example);

    assertRole(rewardManager.readAllowList(exampleAddress), AllowList.Role.None);
    rewardManager.setEnabled(exampleAddress);
    assertRole(rewardManager.readAllowList(exampleAddress), AllowList.Role.Enabled);
  }

  function step_setRewardAddress() public {
    ExampleRewardManager example = new ExampleRewardManager();
    address exampleAddress = address(example);

    rewardManager.setEnabled(exampleAddress);
    example.setRewardAddress(exampleAddress);

    assertEq(example.currentRewardAddress(), exampleAddress);
  }

  function step_setupReceiveFees() public {
    ExampleRewardManager example = new ExampleRewardManager();
    address exampleAddress = address(example);

    rewardManager.setEnabled(exampleAddress);
    example.setRewardAddress(exampleAddress);

    exampleReceiveFees = example;
    exampleBalance = exampleAddress.balance;
  }

  function step_receiveFees() public {
    // used as a noop to test if the correct address receives fees
  }

  function step_checkReceiveFees() public {
    assertGt(address(exampleReceiveFees).balance, exampleBalance);
  }

  function step_areFeeRecipientsAllowed() public {
    ExampleRewardManager example = new ExampleRewardManager();
    assertTrue(!example.areFeeRecipientsAllowed());
  }

  function step_allowFeeRecipients() public {
    ExampleRewardManager example = new ExampleRewardManager();
    address exampleAddress = address(example);

    rewardManager.setEnabled(exampleAddress);

    example.allowFeeRecipients();
    assertTrue(example.areFeeRecipientsAllowed());
  }

  function step_disableRewardAddress() public {
    ExampleRewardManager example = new ExampleRewardManager();
    address exampleAddress = address(example);

    rewardManager.setEnabled(exampleAddress);

    example.setRewardAddress(exampleAddress);

    assertEq(example.currentRewardAddress(), exampleAddress);

    example.disableRewards();

    assertEq(example.currentRewardAddress(), BLACKHOLE_ADDRESS);
  }
}
