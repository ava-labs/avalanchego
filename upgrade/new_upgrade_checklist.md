<!-- markdownlint-disable MD024 -->

# New Upgrade Configuration Checklist

This checklist ists all the required steps when when adding a new network upgrade/fork to AvalancheGo. Note that this list is not exhaustive nor future-proof.

**Example:** Adding upgrade "Ionosphere" after "Helicon"

---

## 1. Core Configuration Files

### [`upgrade/upgrade.go`](./upgrade.go)

- [ ] Add new time field to `Config` struct (e.g., `IonoTime time.Time`)
- [ ] Add `UnscheduledActivationTime` activation time to all configs:
  - `Mainnet` config
  - `Fuji` config
  - `Default` config
- [ ] Add field to `Validate()` method's `upgrades` slice
- [ ] Add `IsXActivated(time.Time) bool` method

### [`vms/rpcchainvm/vm_client.go`](../vms/rpcchainvm/vm_client.go)

- [ ] Add new time field to `NetworkUpgrades` struct

### [`vms/rpcchainvm/vm_server.go`](../vms/rpcchainvm/vm_server.go)

- [ ] Add a new `grpcutils.TimestampAsTime` block to `convertNetworkUpgrades()` function
- [ ] Return the new time in the same function (e.g., `IonoTime time.Time`)

## 2. Test Helper Files

### [`upgrade/upgradetest/fork.go`](./upgradetest/fork.go)

- [ ] Add new fork constant before `Latest`
- [ ] Add case to `String()` method

### [`upgrade/upgradetest/config.go`](./upgradetest/config.go)

- [ ] Add case to `SetTimesTo()` function

---

## After Initial Implementation

Once the upgrade is scheduled for mainnet or fuji, ensure you update the config with the actual activation time respectively. 

