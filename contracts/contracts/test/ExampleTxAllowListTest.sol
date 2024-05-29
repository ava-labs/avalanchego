//SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "../ExampleTxAllowList.sol";
import "../AllowList.sol";
import "../interfaces/IAllowList.sol";
import "./AllowListTest.sol";

contract ExampleTxAllowListTest is AllowListTest {
  address constant OTHER_ADDRESS = 0x0Fa8EA536Be85F32724D57A37758761B86416123;

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

  function step_exampleAllowListReturnsTestIsAdmin() public {
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

  function step_managerCanAllow() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList manager = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address managerAddress = address(manager);

    assertTrue(!example.isEnabled(exampleAddress));
    assertTrue(!example.isManager(managerAddress));

    allowList.setManager(managerAddress);

    assertTrue(manager.isManager(managerAddress));

    manager.setEnabled(exampleAddress);
    assertTrue(example.isEnabled(exampleAddress));
  }

  function step_managerCanRevoke() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList manager = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address managerAddress = address(manager);

    assertTrue(!example.isAdmin(exampleAddress));
    assertTrue(!example.isManager(managerAddress));

    allowList.setEnabled(exampleAddress);
    allowList.setManager(managerAddress);

    assertTrue(example.isEnabled(exampleAddress));
    assertTrue(manager.isManager(managerAddress));

    manager.revoke(exampleAddress);
    assertTrue(!example.isEnabled(exampleAddress));
  }

  function step_managerCannotRevokeAdmin() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList manager = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address managerAddress = address(manager);

    assertTrue(!example.isAdmin(exampleAddress));
    assertTrue(!example.isManager(managerAddress));

    allowList.setAdmin(exampleAddress);
    allowList.setManager(managerAddress);

    assertTrue(example.isAdmin(exampleAddress));
    assertTrue(manager.isManager(managerAddress));

    try manager.revoke(managerAddress) {
      assertTrue(false, "revoke should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected

    try manager.revoke(exampleAddress) {
      assertTrue(false, "revoke should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected

    // state should not have changed when revoke fails
    assertTrue(example.isAdmin(exampleAddress));
    assertTrue(manager.isManager(managerAddress));
  }

  function step_managerCannotGrantAdmin() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList manager = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address managerAddress = address(manager);

    assertTrue(!example.isAdmin(exampleAddress));
    assertTrue(!example.isManager(managerAddress));

    allowList.setManager(managerAddress);

    assertTrue(manager.isManager(managerAddress));

    try manager.setAdmin(exampleAddress) {
      assertTrue(false, "setAdmin should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected

    // state should not have changed when setAdmin fails
    assertTrue(!example.isAdmin(exampleAddress));
    assertTrue(manager.isManager(managerAddress));
  }

  function step_managerCannotGrantManager() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList manager = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address managerAddress = address(manager);

    allowList.setManager(managerAddress);

    assertTrue(!example.isManager(exampleAddress));
    assertTrue(manager.isManager(managerAddress));

    try manager.setManager(exampleAddress) {
      assertTrue(false, "setManager should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected

    // state should not have changed when setManager fails
    assertTrue(!example.isManager(exampleAddress));
    assertTrue(manager.isManager(managerAddress));
  }

  function step_managerCannotRevokeManager() public {
    ExampleTxAllowList example = new ExampleTxAllowList();
    ExampleTxAllowList manager = new ExampleTxAllowList();
    address exampleAddress = address(example);
    address managerAddress = address(manager);

    allowList.setManager(exampleAddress);
    allowList.setManager(managerAddress);

    assertTrue(example.isManager(exampleAddress));
    assertTrue(manager.isManager(managerAddress));

    try manager.revoke(exampleAddress) {
      assertTrue(false, "revoke should fail");
    } catch {} // TODO should match on an error to make sure that this is failing in the way that's expected

    // state should not have changed when revoke fails
    assertTrue(example.isManager(exampleAddress));
    assertTrue(manager.isManager(managerAddress));
  }

  function step_managerCanDeploy() public {
    ExampleTxAllowList manager = new ExampleTxAllowList();
    address managerAddress = address(manager);

    allowList.setManager(managerAddress);

    manager.deployContract();
  }
}
