# Changelog

All notable changes to this project will be documented in this file.

## [0.0.15] - 2025-11-18

### 🚀 Features

- Merge key-value range into trie ([#1427](https://github.com/ava-labs/firewood/pull/1427))
- *(storage)* Replace `ArcSwap` with `Mutex` for better performance ([#1447](https://github.com/ava-labs/firewood/pull/1447))
- *(ffi)* [**breaking**] Remove unused kvBackend interface ([#1448](https://github.com/ava-labs/firewood/pull/1448))
- *(ffi)* Add keepalive struct to own waitgroup logic ([#1437](https://github.com/ava-labs/firewood/pull/1437))
- Remove the last bits of arc-swap ([#1464](https://github.com/ava-labs/firewood/pull/1464))
- *(ffi)* Fill in ffi methods for range proofs ([#1429](https://github.com/ava-labs/firewood/pull/1429))
- *(firewood/ffi)* Add `FjallStore` ([#1395](https://github.com/ava-labs/firewood/pull/1395))

### 🚜 Refactor

- *(rootstore)* Replace RootStoreError with boxed error ([#1446](https://github.com/ava-labs/firewood/pull/1446))
- Remove default 1-minute timeout on `Database.Close()` and set 1-sec in tests ([#1458](https://github.com/ava-labs/firewood/pull/1458))

### 📚 Documentation

- Add agent instructions ([#1445](https://github.com/ava-labs/firewood/pull/1445))
- *(firewood)* Clean up commit steps ([#1453](https://github.com/ava-labs/firewood/pull/1453))
- Merge code review process into CONTRIBUTING.md ([#1397](https://github.com/ava-labs/firewood/pull/1397))
- Add comprehensive metrics documentation in METRICS.md ([#1402](https://github.com/ava-labs/firewood/pull/1402))

### ⚡ Performance

- *(ffi)* [**breaking**] Use fixed size Hash ([#1449](https://github.com/ava-labs/firewood/pull/1449))

### 🧪 Testing

- Fix `giant_node` test ([#1465](https://github.com/ava-labs/firewood/pull/1465))

### ⚙️ Miscellaneous Tasks

- *(ci)* Re-enable ffi-nix job ([#1450](https://github.com/ava-labs/firewood/pull/1450))
- Relegate build equivalent check to post-merge job ([#1469](https://github.com/ava-labs/firewood/pull/1469))

## [0.0.14] - 2025-11-07

### 🚀 Features

- *(monitoring)* Grafana automatic dashboard and auth provisioning ([#1307](https://github.com/ava-labs/firewood/pull/1307))
- *(ffi)* Implement revision handles and expose to FFI layer ([#1326](https://github.com/ava-labs/firewood/pull/1326))
- *(ffi-iterator)* Implementation of Iterator in Rust (1/4) ([#1255](https://github.com/ava-labs/firewood/pull/1255))
- *(ffi-iterator)* Implementation of Iterator in Go (2/4) ([#1256](https://github.com/ava-labs/firewood/pull/1256))
- *(1/7)* Add U4 type to be used as a path component ([#1336](https://github.com/ava-labs/firewood/pull/1336))
- *(2/7)* Add newtype for PathComponent ([#1337](https://github.com/ava-labs/firewood/pull/1337))
- *(3/7)* Add TriePath trait and path abstraction ([#1338](https://github.com/ava-labs/firewood/pull/1338))
- *(4/7)* Add SplitPath trait ([#1339](https://github.com/ava-labs/firewood/pull/1339))
- *(ffi-iterator)* Batching support for Iterator (3/4) ([#1257](https://github.com/ava-labs/firewood/pull/1257))
- *(5/7)* Add PackedPathRef type ([#1341](https://github.com/ava-labs/firewood/pull/1341))
- *(6/7)* Children newtype ([#1344](https://github.com/ava-labs/firewood/pull/1344))
- Use PathComponent in proofs ([#1359](https://github.com/ava-labs/firewood/pull/1359))
- Replace NodeAndPrefix with HashableShunt ([#1362](https://github.com/ava-labs/firewood/pull/1362))
- Parallel updates to Merkle trie (v2) ([#1258](https://github.com/ava-labs/firewood/pull/1258))
- Parallel hashing of Merkle trie ([#1303](https://github.com/ava-labs/firewood/pull/1303))
- Add nix flake for ffi ([#1319](https://github.com/ava-labs/firewood/pull/1319))
- Add more info for IO error message ([#1378](https://github.com/ava-labs/firewood/pull/1378))
- Add TrieNode trait and related functionality ([#1363](https://github.com/ava-labs/firewood/pull/1363))
- Add hashed key-value trie implementation ([#1365](https://github.com/ava-labs/firewood/pull/1365))
- Define explicit associated types on Hashable ([#1366](https://github.com/ava-labs/firewood/pull/1366))
- *(ffi)* `Database.Close()` guarantees proposals committed or freed ([#1349](https://github.com/ava-labs/firewood/pull/1349))
- *(ffi)* [**breaking**] Support empty values in Update operations ([#1420](https://github.com/ava-labs/firewood/pull/1420))
- Add just task runner to ensure reproducibility of CI ([#1345](https://github.com/ava-labs/firewood/pull/1345))
- *(ffi)* [**breaking**] Add finalization logic for Revisions ([#1435](https://github.com/ava-labs/firewood/pull/1435))
- *(benchmark/bootstrap)* Add config param ([#1438](https://github.com/ava-labs/firewood/pull/1438))

### 🐛 Bug Fixes

- Explicitly release advisory lock on drop ([#1352](https://github.com/ava-labs/firewood/pull/1352))
- EINTR during iouring calls ([#1354](https://github.com/ava-labs/firewood/pull/1354))
- Revert "feat: Parallel updates to Merkle trie (v2)" ([#1372](https://github.com/ava-labs/firewood/pull/1372))
- Revert "fix: Revert "feat: Parallel updates to Merkle trie (v2)"" ([#1374](https://github.com/ava-labs/firewood/pull/1374))
- *(storage)* Flush freelist early to prevent corruption ([#1389](https://github.com/ava-labs/firewood/pull/1389))
- *(benchmark/bootstrap)* Consistent go deps ([#1436](https://github.com/ava-labs/firewood/pull/1436))

### 🚜 Refactor

- Unify root storage using Child enum ([#1330](https://github.com/ava-labs/firewood/pull/1330))
- *(db)* `TestDb` constructor ([#1351](https://github.com/ava-labs/firewood/pull/1351))

### 📚 Documentation

- *(benchmark/bootstrap)* Expand README.md ([#1421](https://github.com/ava-labs/firewood/pull/1421))

### 🧪 Testing

- Show symbol diff when ffi-nix build equivalency test fails ([#1423](https://github.com/ava-labs/firewood/pull/1423))

### ⚙️ Miscellaneous Tasks

- Include dependency lockfile ([#1321](https://github.com/ava-labs/firewood/pull/1321))
- Add race detection in Go ([#1323](https://github.com/ava-labs/firewood/pull/1323))
- *(nodestore)* Remove empty committed err ([#1353](https://github.com/ava-labs/firewood/pull/1353))
- Upgrade dependencies ([#1360](https://github.com/ava-labs/firewood/pull/1360))
- *(nodestore)* Access persisted nodestores ([#1355](https://github.com/ava-labs/firewood/pull/1355))
- Update Go to 1.24.8 ([#1361](https://github.com/ava-labs/firewood/pull/1361))
- Print ulimits and set memlock to unlimited before fuzz tests ([#1375](https://github.com/ava-labs/firewood/pull/1375))
- *(db/manager)* Add `MockStore` ([#1346](https://github.com/ava-labs/firewood/pull/1346))
- Update Go to 1.24.9 ([#1380](https://github.com/ava-labs/firewood/pull/1380))
- *(ffi/firewood)* Remove `RootStore` generics ([#1388](https://github.com/ava-labs/firewood/pull/1388))
- Update .golangci.yaml ([#1394](https://github.com/ava-labs/firewood/pull/1394))
- Update ffi build check to configure cargo with same MAKEFLAGS as nix ([#1392](https://github.com/ava-labs/firewood/pull/1392))
- Fix new lint warning from 1.91 update ([#1417](https://github.com/ava-labs/firewood/pull/1417))
- Update .golangci.yaml ([#1419](https://github.com/ava-labs/firewood/pull/1419))
- [**breaking**] Drop binary support for macos 13/14 ([#1425](https://github.com/ava-labs/firewood/pull/1425))
- *(ci)* Disable ffi-nix job pending reliable build equivalency ([#1426](https://github.com/ava-labs/firewood/pull/1426))
- Added helper to reduce code duplication between Db.propose and Proposal.create_proposal ([#1343](https://github.com/ava-labs/firewood/pull/1343))
- Add guidance on Go workspaces ([#1434](https://github.com/ava-labs/firewood/pull/1434))

## [0.0.13] - 2025-09-26

### 🚀 Features

- *(async-removal)* Phase 3 - make `Db` trait sync ([#1213](https://github.com/ava-labs/firewood/pull/1213))
- *(checker)* Fix error with free area that is not head of a free list ([#1231](https://github.com/ava-labs/firewood/pull/1231))
- *(async-removal)* Phase 4 - Make `DbView` synchronous ([#1219](https://github.com/ava-labs/firewood/pull/1219))
- *(ffi-refactor)* Refactor cached view (1/8) ([#1222](https://github.com/ava-labs/firewood/pull/1222))
- *(ffi-refactor)* Add OwnedSlice and OwnedBytes (2/8) ([#1223](https://github.com/ava-labs/firewood/pull/1223))
- *(ffi-refactor)* Introduce VoidResult and panic handlers (3/8) ([#1224](https://github.com/ava-labs/firewood/pull/1224))
- *(ffi-refactor)* Refactor Db opening to use new Result structure (4/8) ([#1225](https://github.com/ava-labs/firewood/pull/1225))
- *(ffi-refactor)* Refactor how hash values are returned (5/8) ([#1226](https://github.com/ava-labs/firewood/pull/1226))
- *(ffi-refactor)* Refactor revision to use database handle (6/8) ([#1227](https://github.com/ava-labs/firewood/pull/1227))
- *(ffi-refactor)* Add `ValueResult` type (7/8) ([#1228](https://github.com/ava-labs/firewood/pull/1228))
- *(checker)* Print report using template for better readability ([#1237](https://github.com/ava-labs/firewood/pull/1237))
- *(checker)* Fix free lists when the erroneous area is the head ([#1240](https://github.com/ava-labs/firewood/pull/1240))
- *(ffi-proofs)* Stub interfaces for FFI to interact with proofs. ([#1253](https://github.com/ava-labs/firewood/pull/1253))
- Explicit impl of PartialEq/Eq on HashOrRlp ([#1260](https://github.com/ava-labs/firewood/pull/1260))
- *(proofs)* [**breaking**] Disable `ValueDigest::Hash` for ethhash ([#1269](https://github.com/ava-labs/firewood/pull/1269))
- [**breaking**] Rename `Hashable::key` ([#1270](https://github.com/ava-labs/firewood/pull/1270))
- *(range-proofs)* KeyValuePairIter (1/2) ([#1282](https://github.com/ava-labs/firewood/pull/1282))
- *(proofs)* [**breaking**] Add v0 serialization for RangeProofs ([#1271](https://github.com/ava-labs/firewood/pull/1271))
- *(ffi-refactor)* Replace sequence id with pointer to proposals (8/8) ([#1221](https://github.com/ava-labs/firewood/pull/1221))

### 🐛 Bug Fixes

- Add an advisory lock ([#1244](https://github.com/ava-labs/firewood/pull/1244))
- Path iterator returned wrong node ([#1259](https://github.com/ava-labs/firewood/pull/1259))
- Use `count` instead of `size_hint` ([#1268](https://github.com/ava-labs/firewood/pull/1268))
- Correct typo in README.md ([#1276](https://github.com/ava-labs/firewood/pull/1276))
- Resolve build failures by pinning opentelemetry to 0.30 ([#1281](https://github.com/ava-labs/firewood/pull/1281))
- *(range-proofs)* Serialize range proof key consistently ([#1278](https://github.com/ava-labs/firewood/pull/1278))
- *(range-proofs)* Fix verify of exclusion proofs ([#1279](https://github.com/ava-labs/firewood/pull/1279))
- *(ffi)* GetFromRoot typo ([#1298](https://github.com/ava-labs/firewood/pull/1298))
- Incorrect gauge metrics ([#1300](https://github.com/ava-labs/firewood/pull/1300))
- M6id is a amd64 machine ([#1305](https://github.com/ava-labs/firewood/pull/1305))
- Revert #1116 ([#1313](https://github.com/ava-labs/firewood/pull/1313))

### 💼 Other

- *(deps)* Update typed-builder requirement from 0.21.0 to 0.22.0 ([#1275](https://github.com/ava-labs/firewood/pull/1275))

### 📚 Documentation

- README implies commit == persist ([#1283](https://github.com/ava-labs/firewood/pull/1283))

### 🧪 Testing

- Mark new_empty_proposal as test only ([#1249](https://github.com/ava-labs/firewood/pull/1249))
- *(firewood)* Use ctor section to init logger for all tests ([#1277](https://github.com/ava-labs/firewood/pull/1277))
- *(bootstrap)* Bootstrap testing scripts ([#1287](https://github.com/ava-labs/firewood/pull/1287))

### ⚙️ Miscellaneous Tasks

- Only allocate the area needed ([#1217](https://github.com/ava-labs/firewood/pull/1217))
- Synchronize .golangci.yaml ([#1234](https://github.com/ava-labs/firewood/pull/1234))
- *(metrics-check)* Re-use previous comment instead of spamming new ones ([#1232](https://github.com/ava-labs/firewood/pull/1232))
- Nuke grpc-testtool ([#1220](https://github.com/ava-labs/firewood/pull/1220))
- Rename FuzzTree ([#1239](https://github.com/ava-labs/firewood/pull/1239))
- Upgrade to rust 1.89 ([#1242](https://github.com/ava-labs/firewood/pull/1242))
- Rename mut_root to root_mut ([#1248](https://github.com/ava-labs/firewood/pull/1248))
- Add missing debug traits ([#1254](https://github.com/ava-labs/firewood/pull/1254))
- These tests actually work now ([#1262](https://github.com/ava-labs/firewood/pull/1262))
- Cargo +nightly clippy --fix ([#1265](https://github.com/ava-labs/firewood/pull/1265))
- Update .golangci.yaml ([#1274](https://github.com/ava-labs/firewood/pull/1274))
- Various script improvements ([#1288](https://github.com/ava-labs/firewood/pull/1288))
- *(bootstrap)* Add keys for brandon ([#1289](https://github.com/ava-labs/firewood/pull/1289))
- *(bootstrap)* Add keys for Bernard and Amin ([#1291](https://github.com/ava-labs/firewood/pull/1291))
- [**breaking**] Decorate enums and structs with `#[non_exhaustive]` ([#1292](https://github.com/ava-labs/firewood/pull/1292))
- Add spot instance support ([#1294](https://github.com/ava-labs/firewood/pull/1294))
- Upgrade go ([#1296](https://github.com/ava-labs/firewood/pull/1296))
- Ask for clippy and rustfmt ([#1306](https://github.com/ava-labs/firewood/pull/1306))
- Add support for enormous disk ([#1308](https://github.com/ava-labs/firewood/pull/1308))
- Disable non-security dependabot version bumps ([#1315](https://github.com/ava-labs/firewood/pull/1315))
- Upgrade dependencies ([#1314](https://github.com/ava-labs/firewood/pull/1314))
- *(benchmark)* Add ssh key ([#1316](https://github.com/ava-labs/firewood/pull/1316))

## [0.0.11] - 2025-08-20

### 🚀 Features

- *(checker)* Checker returns all errors found in the report ([#1176](https://github.com/ava-labs/firewood/pull/1176))
- Remove Default impl on HashType ([#1169](https://github.com/ava-labs/firewood/pull/1169))
- Update revision manager error ([#1170](https://github.com/ava-labs/firewood/pull/1170))
- *(checker)* Return the leaked areas in the checker report ([#1179](https://github.com/ava-labs/firewood/pull/1179))
- *(checker)* Update unaligned page count ([#1181](https://github.com/ava-labs/firewood/pull/1181))
- *(checker)* Add error when node data is bigger than area size ([#1183](https://github.com/ava-labs/firewood/pull/1183))
- Remove `Batch` type alias ([#1171](https://github.com/ava-labs/firewood/pull/1171))
- *(checker)* Annotate IO error with parent pointer in checker errors ([#1188](https://github.com/ava-labs/firewood/pull/1188))
- *(checker)* Do not return physical size to accomodate raw disks ([#1200](https://github.com/ava-labs/firewood/pull/1200))
- *(ffi)* Add BorrowedBytes type ([#1174](https://github.com/ava-labs/firewood/pull/1174))
- *(checker)* More clear print formats for checker report ([#1201](https://github.com/ava-labs/firewood/pull/1201))
- *(async-removal)* Phase 1 - lint on `clippy::unused_async` ([#1211](https://github.com/ava-labs/firewood/pull/1211))
- *(checker)* Collect statistics for branches and leaves separately ([#1206](https://github.com/ava-labs/firewood/pull/1206))
- *(async-removal)* Phase 2 - make `Proposal` trait sync ([#1212](https://github.com/ava-labs/firewood/pull/1212))
- *(checker)* Add checker fix template ([#1199](https://github.com/ava-labs/firewood/pull/1199))

### 🐛 Bug Fixes

- *(checker)* Skip freelist after first encountering an invalid free area ([#1178](https://github.com/ava-labs/firewood/pull/1178))
- Fix race around reading nodes during commit ([#1180](https://github.com/ava-labs/firewood/pull/1180))
- *(fwdctl)* [**breaking**] Db path consistency + no auto-create ([#1189](https://github.com/ava-labs/firewood/pull/1189))

### ⚡ Performance

- Remove unnecessary Box on `OffsetReader` ([#1185](https://github.com/ava-labs/firewood/pull/1185))

### 🧪 Testing

- Add read-during-commit test ([#1186](https://github.com/ava-labs/firewood/pull/1186))
- Fix merkle compatibility test ([#1173](https://github.com/ava-labs/firewood/pull/1173))
- Ban `rand::rng()` and provide an env seeded alternative ([#1192](https://github.com/ava-labs/firewood/pull/1192))
- Reenable eth merkle compatibility test ([#1214](https://github.com/ava-labs/firewood/pull/1214))

### ⚙️ Miscellaneous Tasks

- Metric change detection comments only on 1st-party PRs ([#1167](https://github.com/ava-labs/firewood/pull/1167))
- Run CI on macOS ([#1168](https://github.com/ava-labs/firewood/pull/1168))
- Update .golangci.yaml ([#1166](https://github.com/ava-labs/firewood/pull/1166))
- Allow FreeListIterator to skip to next free list ([#1177](https://github.com/ava-labs/firewood/pull/1177))
- Address lints triggered with rust 1.89 ([#1182](https://github.com/ava-labs/firewood/pull/1182))
- Deny `undocumented-unsafe-blocks` ([#1172](https://github.com/ava-labs/firewood/pull/1172))
- Fwdctl cleanups ([#1190](https://github.com/ava-labs/firewood/pull/1190))
- AreaIndex newtype ([#1193](https://github.com/ava-labs/firewood/pull/1193))
- Remove setup-protoc ([#1203](https://github.com/ava-labs/firewood/pull/1203))
- Automatically label PRs from external contributors ([#1195](https://github.com/ava-labs/firewood/pull/1195))
- Don't fail fast on certain jobs ([#1198](https://github.com/ava-labs/firewood/pull/1198))
- Add PathGuard type when computing hashes ([#1202](https://github.com/ava-labs/firewood/pull/1202))
- *(checker)* Add function to compute area counts and bytes ([#1218](https://github.com/ava-labs/firewood/pull/1218))

## [0.0.10] - 2025-08-01

### 🚀 Features

- *(async-iterator)* Implement ([#1096](https://github.com/ava-labs/firewood/pull/1096))
- Export logs ([#1070](https://github.com/ava-labs/firewood/pull/1070))
- Render the commit sha in fwdctl ([#1109](https://github.com/ava-labs/firewood/pull/1109))
- Update proof types to be generic over mutable or immutable collections ([#1121](https://github.com/ava-labs/firewood/pull/1121))
- Refactor value types to use the type alias ([#1122](https://github.com/ava-labs/firewood/pull/1122))
- *(dumper)* Child links in hex (easy) ([#1124](https://github.com/ava-labs/firewood/pull/1124))
- *(deferred-allocate)* Part 3: Defer allocate ([#1061](https://github.com/ava-labs/firewood/pull/1061))
- *(checker)* Disable buggy ethhash checker ([#1127](https://github.com/ava-labs/firewood/pull/1127))
- Add `Children<T>` type alias ([#1123](https://github.com/ava-labs/firewood/pull/1123))
- Make NodeStore more generic ([#1134](https://github.com/ava-labs/firewood/pull/1134))
- *(checker)* Add progress bar ([#1105](https://github.com/ava-labs/firewood/pull/1105))
- *(checker)* Checker errors include reference to parent ([#1085](https://github.com/ava-labs/firewood/pull/1085))
- Update RangeProof structure ([#1136](https://github.com/ava-labs/firewood/pull/1136))
- Update range_proof signature ([#1151](https://github.com/ava-labs/firewood/pull/1151))
- *(checker)* Add InvalidKey error
- *(deferred-persist)* Part 1: unpersisted gauge ([#1116](https://github.com/ava-labs/firewood/pull/1116))
- *(checker)* Collect basic statistics while checking the db image ([#1149](https://github.com/ava-labs/firewood/pull/1149))
- *(fwdctl)* Add support for dump formats ([#1161](https://github.com/ava-labs/firewood/pull/1161))
- *(ffi)* Remove the Arc wrapper around Proposal ([#1160](https://github.com/ava-labs/firewood/pull/1160))

### 🐛 Bug Fixes

- *(fwdctl)* Fix fwdctl with ethhash ([#1091](https://github.com/ava-labs/firewood/pull/1091))
- *(checker)* Fix checker with ethhash ([#1130](https://github.com/ava-labs/firewood/pull/1130))
- Fix broken deserialization of old FreeArea format ([#1147](https://github.com/ava-labs/firewood/pull/1147))
- Create metrics registration macros ([#980](https://github.com/ava-labs/firewood/pull/980))

### 💼 Other

- Cargo.toml upgrades and fixes ([#1099](https://github.com/ava-labs/firewood/pull/1099))
- *(deps)* Update criterion requirement from 0.6.0 to 0.7.0 ([#1140](https://github.com/ava-labs/firewood/pull/1140))

### 📚 Documentation

- Update ffi/README.md to include configs, metrics, and logs ([#1111](https://github.com/ava-labs/firewood/pull/1111))

### 🎨 Styling

- Remove unnecessary string in error ([#1104](https://github.com/ava-labs/firewood/pull/1104))

### 🧪 Testing

- Add fuzz testing for checker, with fixes ([#1118](https://github.com/ava-labs/firewood/pull/1118))
- Port TestDeepPropose from go->rust ([#1115](https://github.com/ava-labs/firewood/pull/1115))

### ⚙️ Miscellaneous Tasks

- Add propose-on-propose test ([#1097](https://github.com/ava-labs/firewood/pull/1097))
- Implement newtype for LInearAddress ([#1086](https://github.com/ava-labs/firewood/pull/1086))
- Refactor verifying value digests ([#1119](https://github.com/ava-labs/firewood/pull/1119))
- Checker test cleanups ([#1131](https://github.com/ava-labs/firewood/pull/1131))
- Minor cleanups and nits ([#1133](https://github.com/ava-labs/firewood/pull/1133))
- Add a golang install script ([#1141](https://github.com/ava-labs/firewood/pull/1141))
- Move all merkle tests into a subdirectory ([#1150](https://github.com/ava-labs/firewood/pull/1150))
- Require license header for ffi code ([#1159](https://github.com/ava-labs/firewood/pull/1159))
- Bump version to v0.0.10 ([#1165](https://github.com/ava-labs/firewood/pull/1165))

## [0.0.9] - 2025-07-17

### 🚀 Features

- *(ffi)* Add gauges to metrics reporter ([#1035](https://github.com/ava-labs/firewood/pull/1035))
- *(delayed-persist)* Part 1: Roots may be in mem ([#1041](https://github.com/ava-labs/firewood/pull/1041))
- *(delayed-persist)* 2.1: Unpersisted deletions ([#1045](https://github.com/ava-labs/firewood/pull/1045))
- *(delayed-persist)* Part 2.2: Branch Children ([#1047](https://github.com/ava-labs/firewood/pull/1047))
- [**breaking**] Export firewood metrics ([#1044](https://github.com/ava-labs/firewood/pull/1044))
- *(checker)* Add error to report finding leaked areas ([#1052](https://github.com/ava-labs/firewood/pull/1052))
- *(delayed-persist)* Dump unpersisted nodestore ([#1055](https://github.com/ava-labs/firewood/pull/1055))
- *(checker)* Split leaked ranges into valid areas ([#1059](https://github.com/ava-labs/firewood/pull/1059))
- *(checker)* Check for misaligned stored areas ([#1046](https://github.com/ava-labs/firewood/pull/1046))
- [**breaking**] Auto open or create with truncate ([#1064](https://github.com/ava-labs/firewood/pull/1064))
- *(deferred-allocate)* UnpersistedIterator ([#1060](https://github.com/ava-labs/firewood/pull/1060))
- *(checker)* Add hash checks ([#1063](https://github.com/ava-labs/firewood/pull/1063))

### 🐛 Bug Fixes

- Avoid reference to LinearAddress ([#1042](https://github.com/ava-labs/firewood/pull/1042))
- Remove dependency on serde ([#1066](https://github.com/ava-labs/firewood/pull/1066))
- Encoding partial paths for leaf nodes ([#1067](https://github.com/ava-labs/firewood/pull/1067))
- Root_hash_reversed_deletions duplicate keys ([#1076](https://github.com/ava-labs/firewood/pull/1076))
- *(checker)* Avoid checking physical file size for compatibility ([#1079](https://github.com/ava-labs/firewood/pull/1079))

### 🎨 Styling

- Remove unnecessary error descriptor ([#1049](https://github.com/ava-labs/firewood/pull/1049))

### ⚙️ Miscellaneous Tasks

- *(build)* Remove unused dependencies ([#1037](https://github.com/ava-labs/firewood/pull/1037))
- Update firewood in grpc-testtool ([#1040](https://github.com/ava-labs/firewood/pull/1040))
- Aaron is requested only for .github ([#1043](https://github.com/ava-labs/firewood/pull/1043))
- Remove `#[allow]`s no longer needed ([#1022](https://github.com/ava-labs/firewood/pull/1022))
- Split nodestore into functional areas ([#1048](https://github.com/ava-labs/firewood/pull/1048))
- Update `golangci-lint` ([#1053](https://github.com/ava-labs/firewood/pull/1053))
- Update CODEOWNERS ([#1080](https://github.com/ava-labs/firewood/pull/1080))
- Run CI with --no-default-features ([#1081](https://github.com/ava-labs/firewood/pull/1081))
- Release 0.0.9 ([#1084](https://github.com/ava-labs/firewood/pull/1084))

## [0.0.8] - 2025-07-07

### 🚀 Features

- *(checker)* Firewood checker framework ([#936](https://github.com/ava-labs/firewood/pull/936))
- Enable a configurable free list cache in the FFI ([#1017](https://github.com/ava-labs/firewood/pull/1017))
- *(nodestore)* Add functionalities to iterate the free list ([#1015](https://github.com/ava-labs/firewood/pull/1015))
- *(checker)* Traverse free lists ([#1026](https://github.com/ava-labs/firewood/pull/1026))

### 🐛 Bug Fixes

- Unnecessary quotes in publish action ([#996](https://github.com/ava-labs/firewood/pull/996))
- Report IO errors ([#1005](https://github.com/ava-labs/firewood/pull/1005))
- Publish firewood-macros ([#1019](https://github.com/ava-labs/firewood/pull/1019))
- Logger macros causing linting warnings ([#1021](https://github.com/ava-labs/firewood/pull/1021))

### 💼 Other

- *(deps)* Update lru requirement from 0.14.0 to 0.15.0 ([#1001](https://github.com/ava-labs/firewood/pull/1001))
- *(deps)* Update lru requirement from 0.15.0 to 0.16.0 ([#1023](https://github.com/ava-labs/firewood/pull/1023))
- *(deps)* Upgrade sha2, tokio, clap, fastrace, serde... ([#1025](https://github.com/ava-labs/firewood/pull/1025))

### 🚜 Refactor

- *(deps)* Move duplicates to workspace ([#1002](https://github.com/ava-labs/firewood/pull/1002))
- *(ffi)* [**breaking**] Split starting metrics exporter from db startup ([#1016](https://github.com/ava-labs/firewood/pull/1016))

### 📚 Documentation

- README cleanup ([#1024](https://github.com/ava-labs/firewood/pull/1024))

### ⚡ Performance

- Cache the latest view ([#1004](https://github.com/ava-labs/firewood/pull/1004))
- Allow cloned proposals ([#1010](https://github.com/ava-labs/firewood/pull/1010))
- Break up the RevisionManager lock ([#1027](https://github.com/ava-labs/firewood/pull/1027))

### ⚙️ Miscellaneous Tasks

- Suppress clippy::cast_possible_truncation across the workspace ([#1012](https://github.com/ava-labs/firewood/pull/1012))
- Clippy pushdown ([#1011](https://github.com/ava-labs/firewood/pull/1011))
- Allow some extra pedantic warnings ([#1014](https://github.com/ava-labs/firewood/pull/1014))
- Check for metrics changes ([#1013](https://github.com/ava-labs/firewood/pull/1013))
- Share workspace metadata and packages ([#1020](https://github.com/ava-labs/firewood/pull/1020))
- Add concurrency group to attach static libs workflow ([#1038](https://github.com/ava-labs/firewood/pull/1038))
- Bump version to v0.0.8 ([#1018](https://github.com/ava-labs/firewood/pull/1018))

## [0.0.7] - 2025-06-26

### 🚀 Features

- Add methods to fetch views from any hash ([#993](https://github.com/ava-labs/firewood/pull/993))

### 🐛 Bug Fixes

- *(ci)* Include submodule name in ffi tag ([#991](https://github.com/ava-labs/firewood/pull/991))

### ⚡ Performance

- *(metrics)* Add some metrics around propose and commit times ([#989](https://github.com/ava-labs/firewood/pull/989))

### 🎨 Styling

- Use cbindgen to convert to pointers ([#969](https://github.com/ava-labs/firewood/pull/969))

### 🧪 Testing

- Check support for empty proposals ([#988](https://github.com/ava-labs/firewood/pull/988))

### ⚙️ Miscellaneous Tasks

- Simplify + cleanup generate_cgo script ([#979](https://github.com/ava-labs/firewood/pull/979))
- Update Cargo.toml add repository field ([#987](https://github.com/ava-labs/firewood/pull/987))
- *(fuzz)* Add step to upload fuzz testdata on failure ([#990](https://github.com/ava-labs/firewood/pull/990))
- Add special case for non semver tags to attach static libs ([#992](https://github.com/ava-labs/firewood/pull/992))
- Remove requirement for conventional commits ([#994](https://github.com/ava-labs/firewood/pull/994))
- Release v0.0.7 ([#997](https://github.com/ava-labs/firewood/pull/997))

## [0.0.6] - 2025-06-21

### 🚀 Features

- Improve error handling and add sync iterator ([#941](https://github.com/ava-labs/firewood/pull/941))
- *(metrics)* Add read_node counters ([#947](https://github.com/ava-labs/firewood/pull/947))
- Return database creation errors through FFI ([#945](https://github.com/ava-labs/firewood/pull/945))
- *(ffi)* Add go generate switch between enabled cgo blocks ([#978](https://github.com/ava-labs/firewood/pull/978))

### 🐛 Bug Fixes

- Use saturating subtraction for metrics counter ([#937](https://github.com/ava-labs/firewood/pull/937))
- *(attach-static-libs)* Push commit/branch to remote on tag events ([#944](https://github.com/ava-labs/firewood/pull/944))
- Add add_arithmetic_side_effects clippy ([#949](https://github.com/ava-labs/firewood/pull/949))
- Improve ethhash warning message ([#961](https://github.com/ava-labs/firewood/pull/961))
- *(storage)* Parse and validate database versions ([#964](https://github.com/ava-labs/firewood/pull/964))

### 💼 Other

- *(deps)* Update fastrace-opentelemetry requirement from 0.11.0 to 0.12.0 ([#943](https://github.com/ava-labs/firewood/pull/943))
- Move lints to the workspace ([#957](https://github.com/ava-labs/firewood/pull/957))

### ⚡ Performance

- Remove some unecessary allocs during serialization ([#965](https://github.com/ava-labs/firewood/pull/965))

### 🎨 Styling

- *(attach-static-libs)* Use go mod edit instead of sed to update mod path ([#946](https://github.com/ava-labs/firewood/pull/946))

### 🧪 Testing

- *(ethhash)* Convert ethhash test to fuzz test for ethhash compatibility ([#956](https://github.com/ava-labs/firewood/pull/956))

### ⚙️ Miscellaneous Tasks

- Upgrade actions/checkout ([#939](https://github.com/ava-labs/firewood/pull/939))
- Add push to main to attach static libs triggers ([#952](https://github.com/ava-labs/firewood/pull/952))
- Check the PR title for conventional commits ([#953](https://github.com/ava-labs/firewood/pull/953))
- Add Brandon to CODEOWNERS ([#954](https://github.com/ava-labs/firewood/pull/954))
- Set up for publishing to crates.io ([#962](https://github.com/ava-labs/firewood/pull/962))
- Remove remnants of no-std ([#968](https://github.com/ava-labs/firewood/pull/968))
- *(ffi)* Rename ffi package to match dir ([#971](https://github.com/ava-labs/firewood/pull/971))
- *(attach-static-libs)* Add pre build command to set MACOSX_DEPLOYMENT_TARGET for static libs build ([#973](https://github.com/ava-labs/firewood/pull/973))
- Use new firewood-go-* FFI repo naming ([#975](https://github.com/ava-labs/firewood/pull/975))
- Upgrade metrics packages ([#982](https://github.com/ava-labs/firewood/pull/982))
- Release v0.0.6 ([#985](https://github.com/ava-labs/firewood/pull/985))

## [0.0.5] - 2025-06-05

### 🚀 Features

- *(ffi)* Ffi error messages ([#860](https://github.com/ava-labs/firewood/pull/860))
- *(ffi)* Proposal creation isolated from committing ([#867](https://github.com/ava-labs/firewood/pull/867))
- *(ffi)* Get values from proposals ([#877](https://github.com/ava-labs/firewood/pull/877))
- *(ffi)* Full proposal support ([#878](https://github.com/ava-labs/firewood/pull/878))
- *(ffi)* Support `Get` for historical revisions ([#881](https://github.com/ava-labs/firewood/pull/881))
- *(ffi)* Add proposal root retrieval ([#910](https://github.com/ava-labs/firewood/pull/910))

### 🐛 Bug Fixes

- *(ffi)* Prevent memory leak and tips for finding leaks ([#862](https://github.com/ava-labs/firewood/pull/862))
- *(src)* Drop unused revisions ([#866](https://github.com/ava-labs/firewood/pull/866))
- *(ffi)* Clarify roles of `Value` extractors ([#875](https://github.com/ava-labs/firewood/pull/875))
- *(ffi)* Check revision is available ([#890](https://github.com/ava-labs/firewood/pull/890))
- *(ffi)* Prevent undefined behavior on empty slices ([#894](https://github.com/ava-labs/firewood/pull/894))
- Fix empty hash values ([#925](https://github.com/ava-labs/firewood/pull/925))

### 💼 Other

- *(deps)* Update pprof requirement from 0.12.1 to 0.13.0 ([#283](https://github.com/ava-labs/firewood/pull/283))
- *(deps)* Update lru requirement from 0.11.0 to 0.12.0 ([#306](https://github.com/ava-labs/firewood/pull/306))
- *(deps)* Update typed-builder requirement from 0.16.0 to 0.17.0 ([#320](https://github.com/ava-labs/firewood/pull/320))
- *(deps)* Update typed-builder requirement from 0.17.0 to 0.18.0 ([#324](https://github.com/ava-labs/firewood/pull/324))
- Remove dead code ([#333](https://github.com/ava-labs/firewood/pull/333))
- Kv_dump should be done with the iterator ([#347](https://github.com/ava-labs/firewood/pull/347))
- Add remaining lint checks ([#397](https://github.com/ava-labs/firewood/pull/397))
- Finish error handler mapper ([#421](https://github.com/ava-labs/firewood/pull/421))
- Switch from EmptyDB to Db ([#422](https://github.com/ava-labs/firewood/pull/422))
- *(deps)* Update aquamarine requirement from 0.3.1 to 0.4.0 ([#434](https://github.com/ava-labs/firewood/pull/434))
- *(deps)* Update serial_test requirement from 2.0.0 to 3.0.0 ([#477](https://github.com/ava-labs/firewood/pull/477))
- *(deps)* Update aquamarine requirement from 0.4.0 to 0.5.0 ([#496](https://github.com/ava-labs/firewood/pull/496))
- *(deps)* Update env_logger requirement from 0.10.1 to 0.11.0 ([#502](https://github.com/ava-labs/firewood/pull/502))
- *(deps)* Update tonic-build requirement from 0.10.2 to 0.11.0 ([#522](https://github.com/ava-labs/firewood/pull/522))
- *(deps)* Update tonic requirement from 0.10.2 to 0.11.0 ([#523](https://github.com/ava-labs/firewood/pull/523))
- *(deps)* Update nix requirement from 0.27.1 to 0.28.0 ([#563](https://github.com/ava-labs/firewood/pull/563))
- Move clippy pragma closer to usage ([#578](https://github.com/ava-labs/firewood/pull/578))
- *(deps)* Update typed-builder requirement from 0.18.1 to 0.19.1 ([#684](https://github.com/ava-labs/firewood/pull/684))
- *(deps)* Update lru requirement from 0.8.0 to 0.12.4 ([#708](https://github.com/ava-labs/firewood/pull/708))
- *(deps)* Update typed-builder requirement from 0.19.1 to 0.20.0 ([#711](https://github.com/ava-labs/firewood/pull/711))
- *(deps)* Bump actions/download-artifact from 3 to 4.1.7 in /.github/workflows ([#715](https://github.com/ava-labs/firewood/pull/715))
- Insert truncated trie
- Allow for trace and no logging
- Add read_for_update
- Revision history should never grow
- Use a more random hash
- Use smallvec to optimize for 16 byte values
- *(deps)* Update aquamarine requirement from 0.5.0 to 0.6.0 ([#727](https://github.com/ava-labs/firewood/pull/727))
- *(deps)* Update thiserror requirement from 1.0.57 to 2.0.3 ([#751](https://github.com/ava-labs/firewood/pull/751))
- *(deps)* Update pprof requirement from 0.13.0 to 0.14.0 ([#750](https://github.com/ava-labs/firewood/pull/750))
- *(deps)* Update metrics-util requirement from 0.18.0 to 0.19.0 ([#765](https://github.com/ava-labs/firewood/pull/765))
- *(deps)* Update cbindgen requirement from 0.27.0 to 0.28.0 ([#767](https://github.com/ava-labs/firewood/pull/767))
- *(deps)* Update bitfield requirement from 0.17.0 to 0.18.1 ([#772](https://github.com/ava-labs/firewood/pull/772))
- *(deps)* Update lru requirement from 0.12.4 to 0.13.0 ([#771](https://github.com/ava-labs/firewood/pull/771))
- *(deps)* Update bitfield requirement from 0.18.1 to 0.19.0 ([#801](https://github.com/ava-labs/firewood/pull/801))
- *(deps)* Update typed-builder requirement from 0.20.0 to 0.21.0 ([#815](https://github.com/ava-labs/firewood/pull/815))
- *(deps)* Update tonic requirement from 0.12.1 to 0.13.0 ([#826](https://github.com/ava-labs/firewood/pull/826))
- *(deps)* Update opentelemetry requirement from 0.28.0 to 0.29.0 ([#816](https://github.com/ava-labs/firewood/pull/816))
- *(deps)* Update lru requirement from 0.13.0 to 0.14.0 ([#840](https://github.com/ava-labs/firewood/pull/840))
- *(deps)* Update metrics-exporter-prometheus requirement from 0.16.1 to 0.17.0 ([#853](https://github.com/ava-labs/firewood/pull/853))
- *(deps)* Update rand requirement from 0.8.5 to 0.9.1 ([#850](https://github.com/ava-labs/firewood/pull/850))
- *(deps)* Update pprof requirement from 0.14.0 to 0.15.0 ([#906](https://github.com/ava-labs/firewood/pull/906))
- *(deps)* Update cbindgen requirement from 0.28.0 to 0.29.0 ([#899](https://github.com/ava-labs/firewood/pull/899))
- *(deps)* Update criterion requirement from 0.5.1 to 0.6.0 ([#898](https://github.com/ava-labs/firewood/pull/898))
- *(deps)* Bump golang.org/x/crypto from 0.17.0 to 0.35.0 in /ffi/tests ([#907](https://github.com/ava-labs/firewood/pull/907))
- *(deps)* Bump google.golang.org/protobuf from 1.27.1 to 1.33.0  /ffi/tests ([#923](https://github.com/ava-labs/firewood/pull/923))
- *(deps)* Bump google.golang.org/protobuf from 1.30.0 to 1.33.0 ([#924](https://github.com/ava-labs/firewood/pull/924))

### 🚜 Refactor

- *(ffi)* Cleanup unused and duplicate code ([#926](https://github.com/ava-labs/firewood/pull/926))

### 📚 Documentation

- *(ffi)* Remove private declarations from public docs ([#874](https://github.com/ava-labs/firewood/pull/874))

### 🧪 Testing

- *(ffi/tests)* Basic eth compatibility ([#825](https://github.com/ava-labs/firewood/pull/825))
- *(ethhash)* Use libevm ([#900](https://github.com/ava-labs/firewood/pull/900))

### ⚙️ Miscellaneous Tasks

- Use `decode` in single key proof verification ([#295](https://github.com/ava-labs/firewood/pull/295))
- Use `decode` in range proof verification ([#303](https://github.com/ava-labs/firewood/pull/303))
- Naming the elements of `ExtNode` ([#305](https://github.com/ava-labs/firewood/pull/305))
- Remove the getter pattern over `ExtNode` ([#310](https://github.com/ava-labs/firewood/pull/310))
- Proof cleanup ([#316](https://github.com/ava-labs/firewood/pull/316))
- *(ffi/tests)* Update go-ethereum v1.15.7 ([#838](https://github.com/ava-labs/firewood/pull/838))
- *(ffi)* Fix typo fwd_close_db comment ([#843](https://github.com/ava-labs/firewood/pull/843))
- *(ffi)* Add linter ([#893](https://github.com/ava-labs/firewood/pull/893))
- Require conventional commit format ([#933](https://github.com/ava-labs/firewood/pull/933))
- Bump to v0.5.0 ([#934](https://github.com/ava-labs/firewood/pull/934))

## [0.0.4] - 2023-09-27

### 🚀 Features

- Identify a revision with root hash ([#126](https://github.com/ava-labs/firewood/pull/126))
- Supports chains of `StoreRevMut` ([#175](https://github.com/ava-labs/firewood/pull/175))
- Add proposal ([#181](https://github.com/ava-labs/firewood/pull/181))

### 🐛 Bug Fixes

- Update release to cargo-workspace-version ([#75](https://github.com/ava-labs/firewood/pull/75))

### 💼 Other

- *(deps)* Update criterion requirement from 0.4.0 to 0.5.1 ([#96](https://github.com/ava-labs/firewood/pull/96))
- *(deps)* Update enum-as-inner requirement from 0.5.1 to 0.6.0 ([#107](https://github.com/ava-labs/firewood/pull/107))
- :position FTW? ([#140](https://github.com/ava-labs/firewood/pull/140))
- *(deps)* Update indexmap requirement from 1.9.1 to 2.0.0 ([#147](https://github.com/ava-labs/firewood/pull/147))
- *(deps)* Update pprof requirement from 0.11.1 to 0.12.0 ([#152](https://github.com/ava-labs/firewood/pull/152))
- *(deps)* Update typed-builder requirement from 0.14.0 to 0.15.0 ([#153](https://github.com/ava-labs/firewood/pull/153))
- *(deps)* Update lru requirement from 0.10.0 to 0.11.0 ([#155](https://github.com/ava-labs/firewood/pull/155))
- Update hash fn to root_hash ([#170](https://github.com/ava-labs/firewood/pull/170))
- Remove generics on Db ([#196](https://github.com/ava-labs/firewood/pull/196))
- Remove generics for Proposal ([#197](https://github.com/ava-labs/firewood/pull/197))
- Use quotes around all ([#200](https://github.com/ava-labs/firewood/pull/200))
- :get<K>: use Nibbles ([#210](https://github.com/ava-labs/firewood/pull/210))
- Variable renames ([#211](https://github.com/ava-labs/firewood/pull/211))
- Use thiserror ([#221](https://github.com/ava-labs/firewood/pull/221))
- *(deps)* Update typed-builder requirement from 0.15.0 to 0.16.0 ([#222](https://github.com/ava-labs/firewood/pull/222))
- *(deps)* Update tonic-build requirement from 0.9.2 to 0.10.0 ([#247](https://github.com/ava-labs/firewood/pull/247))
- *(deps)* Update prost requirement from 0.11.9 to 0.12.0 ([#246](https://github.com/ava-labs/firewood/pull/246))

### ⚙️ Miscellaneous Tasks

- Refactor `rev.rs` ([#74](https://github.com/ava-labs/firewood/pull/74))
- Disable `test_buffer_with_redo` ([#128](https://github.com/ava-labs/firewood/pull/128))
- Verify concurrent committing write batches ([#172](https://github.com/ava-labs/firewood/pull/172))
- Remove redundant code ([#174](https://github.com/ava-labs/firewood/pull/174))
- Remove unused clone for `StoreRevMutDelta` ([#178](https://github.com/ava-labs/firewood/pull/178))
- Abstract out mutable store creation ([#176](https://github.com/ava-labs/firewood/pull/176))
- Proposal test cleanup ([#184](https://github.com/ava-labs/firewood/pull/184))
- Add comments for `Proposal` ([#186](https://github.com/ava-labs/firewood/pull/186))
- Deprecate `WriteBatch` and use `Proposal` instead ([#188](https://github.com/ava-labs/firewood/pull/188))
- Inline doc clean up ([#240](https://github.com/ava-labs/firewood/pull/240))
- Remove unused blob in db ([#245](https://github.com/ava-labs/firewood/pull/245))
- Add license header to firewood files ([#262](https://github.com/ava-labs/firewood/pull/262))
- Revert back `test_proof` changes accidentally changed ([#279](https://github.com/ava-labs/firewood/pull/279))

## [0.0.3] - 2023-04-28

### 💼 Other

- Move benching to criterion ([#61](https://github.com/ava-labs/firewood/pull/61))
- Refactor file operations to use a Path ([#26](https://github.com/ava-labs/firewood/pull/26))
- Fix panic get_item on a dirty write ([#66](https://github.com/ava-labs/firewood/pull/66))
- Improve error handling ([#70](https://github.com/ava-labs/firewood/pull/70))

### 🧪 Testing

- Speed up slow unit tests ([#58](https://github.com/ava-labs/firewood/pull/58))

### ⚙️ Miscellaneous Tasks

- Add backtrace to e2e tests ([#59](https://github.com/ava-labs/firewood/pull/59))

## [0.0.2] - 2023-04-21

### 💼 Other

- Fix test flake ([#44](https://github.com/ava-labs/firewood/pull/44))

### 📚 Documentation

- Add release notes ([#27](https://github.com/ava-labs/firewood/pull/27))
- Update CODEOWNERS ([#28](https://github.com/ava-labs/firewood/pull/28))
- Add badges to README ([#33](https://github.com/ava-labs/firewood/pull/33))

## [0.0.1] - 2023-04-14

### 🐛 Bug Fixes

- Clippy linting
- Specificy --lib in rustdoc linters
- Unset the pre calculated RLP values of interval nodes
- Run cargo clippy --fix
- Handle empty key value proof arguments as an error
- Tweak repo organization ([#130](https://github.com/ava-labs/firewood/pull/130))
- Run clippy --fix across all workspaces ([#149](https://github.com/ava-labs/firewood/pull/149))
- Update StoreError to use thiserror ([#156](https://github.com/ava-labs/firewood/pull/156))
- Update db::new() to accept a Path ([#187](https://github.com/ava-labs/firewood/pull/187))
- Use bytemuck instead of unsafe in growth-ring ([#185](https://github.com/ava-labs/firewood/pull/185))
- Update firewood sub-projects ([#16](https://github.com/ava-labs/firewood/pull/16))

### 💼 Other

- Fix additional clippy warnings
- Additional clippy fixes
- Fix additional clippy warnings
- Fix outstanding lint issues
- *(deps)* Update nix requirement from 0.25.0 to 0.26.1
- Update version to 0.0.1
- Add usage examples
- Add fwdctl create command
- Add fwdctl README and test
- Fix flag arguments; add fwdctl documentation
- Add logger
- Use log-level flag for setting logging level
- *(deps)* Update lru requirement from 0.8.0 to 0.9.0
- Add generic key value insertion command
- Add get command
- Add delete command
- Move cli tests under tests/
- Only use kv_ functions in fwdctl
- Fix implementation and add tests
- Add exit codes and stderr error logging
- Add tests
- Add serial library for testing purposes
- Add root command
- Add dump command
- Fixup root tests to be serial
- *(deps)* Update typed-builder requirement from 0.11.0 to 0.12.0
- Add VSCode
- Update merkle_utils to return Results
- Fixup command UX to be positional
- Update firewood to match needed functionality
- Update DB and Merkle errors to implement the Error trait
- Update proof errors
- Add StdError trait to ProofError
- *(deps)* Update nix requirement from 0.25.0 to 0.26.2
- *(deps)* Update lru requirement from 0.8.0 to 0.10.0
- *(deps)* Update typed-builder requirement from 0.12.0 to 0.13.0
- *(deps)* Update typed-builder requirement from 0.13.0 to 0.14.0 ([#144](https://github.com/ava-labs/firewood/pull/144))
- Update create_file to return a Result ([#150](https://github.com/ava-labs/firewood/pull/150))
- *(deps)* Update predicates requirement from 2.1.1 to 3.0.1 ([#154](https://github.com/ava-labs/firewood/pull/154))
- Add new library crate ([#158](https://github.com/ava-labs/firewood/pull/158))
- *(deps)* Update serial_test requirement from 1.0.0 to 2.0.0 ([#173](https://github.com/ava-labs/firewood/pull/173))
- Refactor kv_remove to be more ergonomic ([#168](https://github.com/ava-labs/firewood/pull/168))
- Add e2e test ([#167](https://github.com/ava-labs/firewood/pull/167))
- Use eth and proof feature gates across all API surfaces. ([#181](https://github.com/ava-labs/firewood/pull/181))
- Add license header to firewood source code ([#189](https://github.com/ava-labs/firewood/pull/189))

### 📚 Documentation

- Add link to fwdctl README in main README
- Update fwdctl README with storage information
- Update fwdctl README with more examples
- Document get_revisions function with additional information. ([#177](https://github.com/ava-labs/firewood/pull/177))
- Add alpha warning to firewood README ([#191](https://github.com/ava-labs/firewood/pull/191))

### 🧪 Testing

- Add more range proof tests
- Update tests to use Results
- Re-enable integration tests after introduce cargo workspaces

### ⚙️ Miscellaneous Tasks

- Add release and publish GH Actions
- Update batch sizes in ci e2e job
- Add docs linter to strengthen firewood documentation
- Clippy should fail in case of warnings ([#151](https://github.com/ava-labs/firewood/pull/151))
- Fail in case of error publishing firewood crate ([#21](https://github.com/ava-labs/firewood/pull/21))

<!-- generated by git-cliff -->
