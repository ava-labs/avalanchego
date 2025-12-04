//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "./interfaces/IRewardManager.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

address constant REWARD_MANAGER_ADDRESS = 0x0200000000000000000000000000000000000004;

// ExampleRewardManager is a sample wrapper contract for RewardManager precompile.
contract ExampleRewardManager is Ownable {
  IRewardManager rewardManager = IRewardManager(REWARD_MANAGER_ADDRESS);

  constructor() Ownable(msg.sender) {}

  function currentRewardAddress() public view returns (address) {
    return rewardManager.currentRewardAddress();
  }

  function setRewardAddress(address addr) public onlyOwner {
    rewardManager.setRewardAddress(addr);
  }

  function allowFeeRecipients() public onlyOwner {
    rewardManager.allowFeeRecipients();
  }

  function disableRewards() public onlyOwner {
    rewardManager.disableRewards();
  }

  function areFeeRecipientsAllowed() public view returns (bool) {
    return rewardManager.areFeeRecipientsAllowed();
  }
}
