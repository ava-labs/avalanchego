/*
 *
 * The `test` function is a wrapper around Mocha's `it` function. It provides a normalized framework for running the
 * majority of your test assertions inside of a smart-contract, using `DS-Test`.
 * The API can be used as follows (all of the examples are equivalent):
 * ```ts
 * test("<test_name>", "<contract_function_name>")
 * test("<test_name>", ["<contract_function_name>"])
 * test("<test_name>", { method: "<contract_function_name>", overrides: {}, shouldFail: false, debug: false })
 * test("<test_name>", [{ method: "<contract_function_name>", overrides: {}, shouldFail: false, debug: false }])
 * test("<test_name>", [{ method: "<contract_function_name>", shouldFail: false, debug: false }], {})
 * ```
 * Many contract functions can be called as a part of the same test:
 * ```ts
 * test("<test_name>", ["<step_fn1>", "<step_fn2>", "<step_fn3>"])
 * ```
 * Individual test functions can describe their own overrides with the `overrides` property.
 * If an object is passed in as the third argument to `test`, it will be used as the default overrides for all test
 * functions.
 * The following are equivalent:
 * ```ts
 * test("<test_name>", [{ method: "<contract_function_name>", overrides: { from: "0x123" } }])
 * test("<test_name>", [{ method: "<contract_function_name>" }], { from: "0x123" })
 * ```
 * In the above cases, the `from` override must be a signer.
 * The `shouldFail` property can be used to indicate that the test function should fail. This should be used sparingly
 * as it is not possible to match on the failure reason.
 * Furthermore, the `debug` property can be used to print any thrown errors when attempting to
 * send a transaction or while waiting for the transaction to be confirmed (the transaction is the smart contract call).
 * `debug` will also cause any parseable event logs to be printed that start with the `log_` prefix.
 * `DSTest` contracts have several options for emitting `log_` events.
 *
 */

// Below are the types that help define all the different ways to call `test`

export const Roles = {
  None: 0,
  Enabled: 1,
  Admin: 2,
  Manager: 3,
}