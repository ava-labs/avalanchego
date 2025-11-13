<!-- markdownlint-disable MD024 -->

# New Upgrade Configuration Checklist

This checklist documents all steps required when adding a new network upgrade/fork to AvalancheGo.

**Example:** Adding upgrade "Ionosphere" after "Helicon"

---

## For Implementers

### 1. Core Configuration Files

#### [`upgrade/upgrade.go`](./upgrade.go)

- [ ] Add new time field to `Config` struct (e.g., `IonoTime time.Time`)
  - [ ] Use `Time` suffix for consistency
  - [ ] Add JSON tag in camelCase (e.g., `json:"ionosphereTime"`)
  - [ ] Place field in chronological order after the previous upgrade

- [ ] Add activation time to `Mainnet` config
  - [ ] Set to `UnscheduledActivationTime` initially
  - [ ] Update with actual mainnet activation time when scheduled

- [ ] Add activation time to `Fuji` config
  - [ ] Set to `UnscheduledActivationTime` initially
  - [ ] Update with actual fuji activation time when scheduled

- [ ] Add activation time to `Default` config
  - [ ] Typically set to `UnscheduledActivationTime` for new upgrades

- [ ] Add field to `Validate()` method's `upgrades` slice
  - [ ] Must be in chronological order to ensure proper validation

- [ ] Add `IsXActivated(time.Time) bool` method
  - [ ] Follow naming pattern: `Is[UpgradeName]Activated`
  - [ ] Use standard implementation: `return !t.Before(c.[UpgradeName]Time)`

- [ ] If the upgrade has associated configuration (like `GraniteEpochDuration`):
  - [ ] Add configuration field(s) to `Config` struct
  - [ ] Add to all three configs (`Mainnet`, `Fuji`, `Default`)
  - [ ] Update `Validate()` if validation is needed

### 2. Test Helper Files

#### [`upgrade/upgradetest/fork.go`](./upgradetest/fork.go)

- [ ] Add new fork constant before `Latest`
  - [ ] Use the upgrade name (e.g., `Ionosphere`)
  - [ ] Place in chronological order

- [ ] Update `Latest` constant to point to the new fork
  - [ ] Change `Latest = [PreviousFork]` to `Latest = [NewFork]`

- [ ] Add case to `String()` method
  - [ ] Follow alphabetical/chronological pattern
  - [ ] Return the exact upgrade name as a string

- [ ] Add case to `FromString()` function
  - [ ] Map the string name back to the fork constant
  - [ ] Must match the string returned by `String()`

#### [`upgrade/upgradetest/config.go`](./upgradetest/config.go)

- [ ] Add case to `SetTimesTo()` function
  - [ ] Add at the TOP of the switch statement (newest first)
  - [ ] Set the new upgrade time field
  - [ ] Format:

    ```go
    case Ionosphere:
        c.IonosphereTime = upgradeTime
        fallthrough
    ```

---

## For Reviewers

Use this checklist to verify the implementation is complete:

### Core Files Review

#### [`upgrade/upgrade.go`](./upgrade.go)

- [ ] New time field added to `Config` struct
  - [ ] Uses `Time` suffix
  - [ ] Has proper JSON tag in camelCase
  - [ ] In chronological order
- [ ] Field added to all three configs: `Mainnet`, `Fuji`, `Default`
  - [ ] `Mainnet` and `Fuji`: Set to `UnscheduledActivationTime` (or scheduled time if known)
  - [ ] `Default`: Set appropriately for local testing
- [ ] New field added to `Validate()` method in correct order
- [ ] `IsXActivated()` method added with correct implementation
- [ ] Any upgrade-specific config fields properly added and initialized

#### [`upgrade/upgradetest/fork.go`](./upgradetest/fork.go)

- [ ] New fork constant added in chronological order before `Latest`
- [ ] `Latest` constant updated to new fork
- [ ] New case in `String()` method returns correct string
- [ ] New case in `FromString()` function maps string to constant

#### [`upgrade/upgradetest/config.go`](./upgradetest/config.go)

- [ ] New case added to `SetTimesTo()` at the TOP of switch

---

## After Initial Implementation

Once the upgrade is scheduled for mainnet/fuji:

- [ ] Update `Mainnet` config with actual activation time
- [ ] Update `Fuji` config with actual activation time  
- [ ] Update any associated configuration values if they differ from initial values
