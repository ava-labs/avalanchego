//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;
import "precompile/allowlist/allowlisttest/bindings/IAllowList.sol";

interface INativeMinter is IAllowList {
  event NativeCoinMinted(address indexed sender, address indexed recipient, uint256 amount);
  // Mint [amount] number of native coins and send to [addr]
  function mintNativeCoin(address addr, uint256 amount) external;
}
