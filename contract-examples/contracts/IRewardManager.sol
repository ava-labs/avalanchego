//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;
import "./IAllowList.sol";

interface IRewardManager is IAllowList {
  // setRewardAddress sets the reward address to the given address
  function setRewardAddress(address addr) external;

  // allowFeeRecipients allows block builders to claim fees
  function allowFeeRecipients() external;

  // disableRewards disables block rewards and starts burning fees
  function disableRewards() external;

  // currentRewardAddress returns the current reward address
  function currentRewardAddress() external view returns (address rewardAddress);

  // areFeeRecipientsAllowed returns true if fee recipients are allowed
  function areFeeRecipientsAllowed() external view returns (bool isAllowed);
}
