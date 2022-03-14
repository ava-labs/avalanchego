//SPDX-License-Identifier: MIT
pragma solidity >=0.6.2;

import "@openzeppelin/contracts/access/Ownable.sol";
import "./IAllowList.sol";

// ExampleDeployerList shows how ContractDeployerAllowList precompile can be used in a smart contract
// All methods of [allowList] can be directly called. There are example calls as tasks in hardhat.config.ts file.
contract ExampleDeployerList is Ownable {
  // Precompiled Allow List Contract Address
  address constant DEPLOYER_LIST = 0x0200000000000000000000000000000000000000;
  IAllowList allowList = IAllowList(DEPLOYER_LIST);

  uint256 constant STATUS_NONE = 0;
  uint256 constant STATUS_ENABLED = 1;
  uint256 constant STATUS_ADMIN = 2;

  constructor() Ownable() {}

  function isAdmin(address addr) public view returns (bool) {
    uint256 result = allowList.readAllowList(addr);
    return result == STATUS_ADMIN;
  }

  function isDeployer(address addr) public view returns (bool) {
    uint256 result = allowList.readAllowList(addr);
    // if address is ENABLED or ADMIN it can deploy
    // in other words, if it's not NONE it can deploy.
    return result != STATUS_NONE;
  }

  function addAdmin(address addr) public onlyOwner {
    allowList.setAdmin(addr);
  }

  function addDeployer(address addr) public onlyOwner {
    allowList.setEnabled(addr);
  }

  function revoke(address addr) public onlyOwner {
    require(_msgSender() != addr, "cannot revoke own role");
    allowList.setNone(addr);
  }
}
