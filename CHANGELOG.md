# Changelog

All notable changes to this project will be documented in this file.

## [0.0.8] - 2025-07-07

### 🚀 Features

- *(checker)* Firewood checker framework (#936)
- Enable a configurable free list cache in the FFI (#1017)
- *(nodestore)* Add functionalities to iterate the free list (#1015)
- *(checker)* Traverse free lists (#1026)

### 🐛 Bug Fixes

- Unnecessary quotes in publish action (#996)
- Report IO errors (#1005)
- Publish firewood-macros (#1019)
- Logger macros causing linting warnings (#1021)

### 💼 Other

- *(deps)* Update lru requirement from 0.14.0 to 0.15.0 (#1001)
- *(deps)* Update lru requirement from 0.15.0 to 0.16.0 (#1023)
- *(deps)* Upgrade sha2, tokio, clap, fastrace, serde... (#1025)

### 🚜 Refactor

- *(deps)* Move duplicates to workspace (#1002)
- *(ffi)* [**breaking**] Split starting metrics exporter from db startup (#1016)

### 📚 Documentation

- README cleanup (#1024)

### ⚡ Performance

- Cache the latest view (#1004)
- Allow cloned proposals (#1010)
- Break up the RevisionManager lock (#1027)

### ⚙️ Miscellaneous Tasks

- Suppress clippy::cast\_possible\_truncation across the workspace (#1012)
- Clippy pushdown (#1011)
- Allow some extra pedantic warnings (#1014)
- Check for metrics changes (#1013)
- Share workspace metadata and packages (#1020)
- Add concurrency group to attach static libs workflow (#1038)
- Bump version to v0.0.8 (#1018)

## [0.0.7] - 2025-06-26

### 🚀 Features

- Add methods to fetch views from any hash (#993)

### 🐛 Bug Fixes

- *(ci)* Include submodule name in ffi tag (#991)

### ⚡ Performance

- *(metrics)* Add some metrics around propose and commit times (#989)

### 🎨 Styling

- Use cbindgen to convert to pointers (#969)

### 🧪 Testing

- Check support for empty proposals (#988)

### ⚙️ Miscellaneous Tasks

- Simplify + cleanup generate\_cgo script (#979)
- Update Cargo.toml add repository field (#987)
- *(fuzz)* Add step to upload fuzz testdata on failure (#990)
- Add special case for non semver tags to attach static libs (#992)
- Remove requirement for conventional commits (#994)
- Release v0.0.7 (#997)

## [0.0.6] - 2025-06-21

### 🚀 Features

- Improve error handling and add sync iterator (#941)
- *(metrics)* Add read\_node counters (#947)
- Return database creation errors through FFI (#945)
- *(ffi)* Add go generate switch between enabled cgo blocks (#978)

### 🐛 Bug Fixes

- Use saturating subtraction for metrics counter (#937)
- *(attach-static-libs)* Push commit/branch to remote on tag events (#944)
- Add add\_arithmetic\_side\_effects clippy (#949)
- Improve ethhash warning message (#961)
- *(storage)* Parse and validate database versions (#964)

### 💼 Other

- *(deps)* Update fastrace-opentelemetry requirement from 0.11.0 to 0.12.0 (#943)
- Move lints to the workspace (#957)

### ⚡ Performance

- Remove some unecessary allocs during serialization (#965)

### 🎨 Styling

- *(attach-static-libs)* Use go mod edit instead of sed to update mod path (#946)

### 🧪 Testing

- *(ethhash)* Convert ethhash test to fuzz test for ethhash compatibility (#956)

### ⚙️ Miscellaneous Tasks

- Upgrade actions/checkout (#939)
- Add push to main to attach static libs triggers (#952)
- Check the PR title for conventional commits (#953)
- Add Brandon to CODEOWNERS (#954)
- Set up for publishing to crates.io (#962)
- Remove remnants of no-std (#968)
- *(ffi)* Rename ffi package to match dir (#971)
- *(attach-static-libs)* Add pre build command to set MACOSX\_DEPLOYMENT\_TARGET for static libs build (#973)
- Use new firewood-go-* FFI repo naming (#975)
- Upgrade metrics packages (#982)
- Release v0.0.6 (#985)

## [0.0.5] - 2025-06-05

### 🚀 Features

- *(ffi)* Ffi error messages (#860)
- *(ffi)* Proposal creation isolated from committing (#867)
- *(ffi)* Get values from proposals (#877)
- *(ffi)* Full proposal support (#878)
- *(ffi)* Support `Get` for historical revisions (#881)
- *(ffi)* Add proposal root retrieval (#910)

### 🐛 Bug Fixes

- *(ffi)* Prevent memory leak and tips for finding leaks (#862)
- *(src)* Drop unused revisions (#866)
- *(ffi)* Clarify roles of `Value` extractors (#875)
- *(ffi)* Check revision is available (#890)
- *(ffi)* Prevent undefined behavior on empty slices (#894)
- Fix empty hash values (#925)

### 💼 Other

- *(deps)* Update pprof requirement from 0.12.1 to 0.13.0 (#283)
- *(deps)* Update lru requirement from 0.11.0 to 0.12.0 (#306)
- *(deps)* Update typed-builder requirement from 0.16.0 to 0.17.0 (#320)
- *(deps)* Update typed-builder requirement from 0.17.0 to 0.18.0 (#324)
- Remove dead code (#333)
- Kv\_dump should be done with the iterator (#347)
- Add remaining lint checks (#397)
- Finish error handler mapper (#421)
- Switch from EmptyDB to Db (#422)
- *(deps)* Update aquamarine requirement from 0.3.1 to 0.4.0 (#434)
- *(deps)* Update serial\_test requirement from 2.0.0 to 3.0.0 (#477)
- *(deps)* Update aquamarine requirement from 0.4.0 to 0.5.0 (#496)
- *(deps)* Update env\_logger requirement from 0.10.1 to 0.11.0 (#502)
- *(deps)* Update tonic-build requirement from 0.10.2 to 0.11.0 (#522)
- *(deps)* Update tonic requirement from 0.10.2 to 0.11.0 (#523)
- *(deps)* Update nix requirement from 0.27.1 to 0.28.0 (#563)
- Move clippy pragma closer to usage (#578)
- *(deps)* Update typed-builder requirement from 0.18.1 to 0.19.1 (#684)
- *(deps)* Update lru requirement from 0.8.0 to 0.12.4 (#708)
- *(deps)* Update typed-builder requirement from 0.19.1 to 0.20.0 (#711)
- *(deps)* Bump actions/download-artifact from 3 to 4.1.7 in /.github/workflows (#715)
- Insert truncated trie
- Allow for trace and no logging
- Add read\_for\_update
- Revision history should never grow
- Use a more random hash
- Use smallvec to optimize for 16 byte values
- *(deps)* Update aquamarine requirement from 0.5.0 to 0.6.0 (#727)
- *(deps)* Update thiserror requirement from 1.0.57 to 2.0.3 (#751)
- *(deps)* Update pprof requirement from 0.13.0 to 0.14.0 (#750)
- *(deps)* Update metrics-util requirement from 0.18.0 to 0.19.0 (#765)
- *(deps)* Update cbindgen requirement from 0.27.0 to 0.28.0 (#767)
- *(deps)* Update bitfield requirement from 0.17.0 to 0.18.1 (#772)
- *(deps)* Update lru requirement from 0.12.4 to 0.13.0 (#771)
- *(deps)* Update bitfield requirement from 0.18.1 to 0.19.0 (#801)
- *(deps)* Update typed-builder requirement from 0.20.0 to 0.21.0 (#815)
- *(deps)* Update tonic requirement from 0.12.1 to 0.13.0 (#826)
- *(deps)* Update opentelemetry requirement from 0.28.0 to 0.29.0 (#816)
- *(deps)* Update lru requirement from 0.13.0 to 0.14.0 (#840)
- *(deps)* Update metrics-exporter-prometheus requirement from 0.16.1 to 0.17.0 (#853)
- *(deps)* Update rand requirement from 0.8.5 to 0.9.1 (#850)
- *(deps)* Update pprof requirement from 0.14.0 to 0.15.0 (#906)
- *(deps)* Update cbindgen requirement from 0.28.0 to 0.29.0 (#899)
- *(deps)* Update criterion requirement from 0.5.1 to 0.6.0 (#898)
- *(deps)* Bump golang.org/x/crypto from 0.17.0 to 0.35.0 in /ffi/tests (#907)
- *(deps)* Bump google.golang.org/protobuf from 1.27.1 to 1.33.0  /ffi/tests (#923)
- *(deps)* Bump google.golang.org/protobuf from 1.30.0 to 1.33.0 (#924)

### 🚜 Refactor

- *(ffi)* Cleanup unused and duplicate code (#926)

### 📚 Documentation

- *(ffi)* Remove private declarations from public docs (#874)

### 🧪 Testing

- *(ffi/tests)* Basic eth compatibility (#825)
- *(ethhash)* Use libevm (#900)

### ⚙️ Miscellaneous Tasks

- Use `decode` in single key proof verification (#295)
- Use `decode` in range proof verification (#303)
- Naming the elements of `ExtNode` (#305)
- Remove the getter pattern over `ExtNode` (#310)
- Proof cleanup (#316)
- *(ffi/tests)* Update go-ethereum v1.15.7 (#838)
- *(ffi)* Fix typo fwd\_close\_db comment (#843)
- *(ffi)* Add linter (#893)
- Require conventional commit format (#933)
- Bump to v0.5.0 (#934)

## [0.0.4] - 2023-09-27

### 🚀 Features

- Identify a revision with root hash (#126)
- Supports chains of `StoreRevMut` (#175)
- Add proposal (#181)

### 🐛 Bug Fixes

- Update release to cargo-workspace-version (#75)

### 💼 Other

- *(deps)* Update criterion requirement from 0.4.0 to 0.5.1 (#96)
- *(deps)* Update enum-as-inner requirement from 0.5.1 to 0.6.0 (#107)
- :position FTW? (#140)
- *(deps)* Update indexmap requirement from 1.9.1 to 2.0.0 (#147)
- *(deps)* Update pprof requirement from 0.11.1 to 0.12.0 (#152)
- *(deps)* Update typed-builder requirement from 0.14.0 to 0.15.0 (#153)
- *(deps)* Update lru requirement from 0.10.0 to 0.11.0 (#155)
- Update hash fn to root\_hash (#170)
- Remove generics on Db (#196)
- Remove generics for Proposal (#197)
- Use quotes around all (#200)
- :get<K>: use Nibbles (#210)
- Variable renames (#211)
- Use thiserror (#221)
- *(deps)* Update typed-builder requirement from 0.15.0 to 0.16.0 (#222)
- *(deps)* Update tonic-build requirement from 0.9.2 to 0.10.0 (#247)
- *(deps)* Update prost requirement from 0.11.9 to 0.12.0 (#246)

### ⚙️ Miscellaneous Tasks

- Refactor `rev.rs` (#74)
- Disable `test\_buffer\_with\_redo` (#128)
- Verify concurrent committing write batches (#172)
- Remove redundant code (#174)
- Remove unused clone for `StoreRevMutDelta` (#178)
- Abstract out mutable store creation (#176)
- Proposal test cleanup (#184)
- Add comments for `Proposal` (#186)
- Deprecate `WriteBatch` and use `Proposal` instead (#188)
- Inline doc clean up (#240)
- Remove unused blob in db (#245)
- Add license header to firewood files (#262)
- Revert back `test\_proof` changes accidentally changed (#279)

## [0.0.3] - 2023-04-28

### 💼 Other

- Move benching to criterion (#61)
- Refactor file operations to use a Path (#26)
- Fix panic get\_item on a dirty write (#66)
- Improve error handling (#70)

### 🧪 Testing

- Speed up slow unit tests (#58)

### ⚙️ Miscellaneous Tasks

- Add backtrace to e2e tests (#59)

## [0.0.2] - 2023-04-21

### 💼 Other

- Fix test flake (#44)

### 📚 Documentation

- Add release notes (#27)
- Update CODEOWNERS (#28)
- Add badges to README (#33)

## [0.0.1] - 2023-04-14

### 🐛 Bug Fixes

- Clippy linting
- Specificy --lib in rustdoc linters
- Unset the pre calculated RLP values of interval nodes
- Run cargo clippy --fix
- Handle empty key value proof arguments as an error
- Tweak repo organization (#130)
- Run clippy --fix across all workspaces (#149)
- Update StoreError to use thiserror (#156)
- Update db::new() to accept a Path (#187)
- Use bytemuck instead of unsafe in growth-ring (#185)
- Update firewood sub-projects (#16)

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
- Only use kv\_ functions in fwdctl
- Fix implementation and add tests
- Add exit codes and stderr error logging
- Add tests
- Add serial library for testing purposes
- Add root command
- Add dump command
- Fixup root tests to be serial
- *(deps)* Update typed-builder requirement from 0.11.0 to 0.12.0
- Add VSCode
- Update merkle\_utils to return Results
- Fixup command UX to be positional
- Update firewood to match needed functionality
- Update DB and Merkle errors to implement the Error trait
- Update proof errors
- Add StdError trait to ProofError
- *(deps)* Update nix requirement from 0.25.0 to 0.26.2
- *(deps)* Update lru requirement from 0.8.0 to 0.10.0
- *(deps)* Update typed-builder requirement from 0.12.0 to 0.13.0
- *(deps)* Update typed-builder requirement from 0.13.0 to 0.14.0 (#144)
- Update create\_file to return a Result (#150)
- *(deps)* Update predicates requirement from 2.1.1 to 3.0.1 (#154)
- Add new library crate (#158)
- *(deps)* Update serial\_test requirement from 1.0.0 to 2.0.0 (#173)
- Refactor kv\_remove to be more ergonomic (#168)
- Add e2e test (#167)
- Use eth and proof feature gates across all API surfaces. (#181)
- Add license header to firewood source code (#189)

### 📚 Documentation

- Add link to fwdctl README in main README
- Update fwdctl README with storage information
- Update fwdctl README with more examples
- Document get\_revisions function with additional information. (#177)
- Add alpha warning to firewood README (#191)

### 🧪 Testing

- Add more range proof tests
- Update tests to use Results
- Re-enable integration tests after introduce cargo workspaces

### ⚙️ Miscellaneous Tasks

- Add release and publish GH Actions
- Update batch sizes in ci e2e job
- Add docs linter to strengthen firewood documentation
- Clippy should fail in case of warnings (#151)
- Fail in case of error publishing firewood crate (#21)

<!-- generated by git-cliff -->
