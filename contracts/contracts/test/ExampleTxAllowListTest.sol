//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "../ExampleTxAllowList.sol";
import "../AllowList.sol";
import "../interfaces/IAllowList.sol";
import "./AllowListTest.sol";

contract ExampleTxAllowListTest is AllowListTest {
  IAllowList allowList = IAllowList(TX_ALLOW_LIST);

  function setUp() public {
    allowList.setNone(OTHER_ADDRESS);
  }

  function step_contractOwnerIsAdmin() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    assertTrue(example.isAdmin(address(this)));
  }

  function step_precompileHasDeployerAsAdmin() public {
    assertRole(allowList.readAllowList(msg.sender), AllowList.Role.Admin);
  }

  function step_newAddressHasNoRole() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    assertRole(allowList.readAllowList(address(example)), AllowList.Role.None);
  }

  function step_noRoleIsNotAdmin() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList other = new ExampleTxAllowList();
    assertTrue(!example.isAdmin(address(other)));
  }

  function step_exmapleAllowListReturnsTestIsAdmin() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    assertTrue(example.isAdmin(address(this)));
  }

  function step_fromOther() public {
    // used as a noop to test transaction-success or failure, depending on wether the signer has been added to the tx-allow-list
  }

  function step_enableOther() public {
    assertRole(allowList.readAllowList(OTHER_ADDRESS), AllowList.Role.None);
    allowList.setEnabled(OTHER_ADDRESS);
  }

  function step_noRoleCannotEnableItself() public {
    ExampleTxAllowList example = new ExampleTxAllowList();

    assertRole(allowList.readAllowList(address(example)), AllowList.Role.None);

    try example.setEnabled(address(example)) {
      assertTrue(false, "setEnabled should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected
  }

  function step_addContractAsAdmin() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    address exampleAddress = address(example);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.None);

    allowList.setAdmin(exampleAddress);

    assertRole(allowList.readAllowList(exampleAddress), AllowList.Role.Admin);

    assertTrue(example.isAdmin(exampleAddress));
  }

  function step_enableThroughContract() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList other = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address otherAddress = address(other);

    assertTrue(!example.isEnabled(exampleAddress));
    assertTrue(!example.isEnabled(otherAddress));

    allowList.setAdmin(exampleAddress);

    assertTrue(example.isEnabled(exampleAddress));
    assertTrue(!example.isEnabled(otherAddress));

    example.setEnabled(otherAddress);

    assertTrue(example.isEnabled(exampleAddress));
    assertTrue(example.isEnabled(otherAddress));
  }

  function step_canDeploy() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    address exampleAddress = address(example);

    allowList.setEnabled(exampleAddress);

    example.deployContract();
  }

  function step_onlyAdminCanEnable() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList other = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address otherAddress = address(other);

    assertTrue(!example.isEnabled(exampleAddress));
    assertTrue(!example.isEnabled(otherAddress));

    allowList.setEnabled(exampleAddress);

    assertTrue(example.isEnabled(exampleAddress));
    assertTrue(!example.isEnabled(otherAddress));

    try example.setEnabled(otherAddress) {
      assertTrue(false, "setEnabled should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected

    // state should not have changed when setEnabled fails
    assertTrue(!example.isEnabled(otherAddress));
  }

  function step_onlyAdminCanRevoke() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList other = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address otherAddress = address(other);

    assertTrue(!example.isEnabled(exampleAddress));
    assertTrue(!example.isEnabled(otherAddress));

    allowList.setEnabled(exampleAddress);
    allowList.setAdmin(otherAddress);

    assertTrue(example.isEnabled(exampleAddress) && !example.isAdmin(exampleAddress));
    assertTrue(example.isAdmin(otherAddress));

    try example.revoke(otherAddress) {
      assertTrue(false, "revoke should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected

    // state should not have changed when revoke fails
    assertTrue(example.isAdmin(otherAddress));
  }

  function step_adminCanRevoke() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList other = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address otherAddress = address(other);

    assertTrue(!example.isEnabled(exampleAddress));
    assertTrue(!example.isEnabled(otherAddress));

    allowList.setAdmin(exampleAddress);
    allowList.setAdmin(otherAddress);

    assertTrue(example.isAdmin(exampleAddress));
    assertTrue(other.isAdmin(otherAddress));

    example.revoke(otherAddress);
    assertTrue(!other.isEnabled(otherAddress));
  }
}
