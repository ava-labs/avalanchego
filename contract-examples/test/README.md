# Testing Precompiles

If you can, put all of your test logic into DS-test tests. Prefix test functions with `step_`. There's also a `setUp` function that gets called before the test contract is deployed. The current best-practice is to re-deploy one test contract per `test` function called in `*.ts` test definitions. The `setUp` method should be called once, then `step_` functions passed in as the 2nd argument to `test("<description>", <step_function_name_OR_array_of_step_function_names>)` will be called in order. `test.only` and `test.skip` behave the same way as `it.only` and `it.skip`. There's also a `test.debug` that combines `test.only` with some extra event logging (you can use `emit log_string` to help debug Solidity test code).

The `test` function is a wrapper around Mocha's `it` function. It provides a normalized framework for running the
majority of your test assertions inside of a smart-contract, using `DS-Test`.
The API can be used as follows (all of the examples are equivalent):

```ts
test("<test_name>", "<contract_function_name>");

test("<test_name>", ["<contract_function_name>"]);

test("<test_name>", {
  method: "<contract_function_name>",
  overrides: {},
  shouldFail: false,
  debug: false,
});

test("<test_name>", [
  {
    method: "<contract_function_name>",
    overrides: {},
    shouldFail: false,
    debug: false,
  },
]);

test(
  "<test_name>",
  [{ method: "<contract_function_name>", shouldFail: false, debug: false }],
  {}
);
```

Many contract functions can be called as a part of the same test:

```ts
test("<test_name>", ["<step_fn1>", "<step_fn2>", "<step_fn3>"])
```

Individual test functions can describe their own overrides with the `overrides` property.
If an object is passed in as the third argument to `test`, it will be used as the default overrides for all test
functions.
The following are equivalent:

```ts
test("<test_name>", [
  { method: "<contract_function_name>", overrides: { from: "0x123" } },
]);

test("<test_name>", [{ method: "<contract_function_name>" }], {
  from: "0x123",
});
```

In the above cases, the `from` override must be a signer.
The `shouldFail` property can be used to indicate that the test function should fail. This should be used sparingly
as it is not possible to match on the failure reason.
Furthermore, the `debug` property can be used to print any thrown errors when attempting to
send a transaction or while waiting for the transaction to be confirmed (the transaction is the smart contract call).
`debug` will also cause any parseable event logs to be printed that start with the `log_` prefix.
`DSTest` contracts have several options for emitting `log_` events.
