import { ethers } from "hardhat"
import { Overrides } from "ethers"
import assert from "assert"

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
type FnNameOrObject = string | string[] | MethodObject | MethodObject[]

// Limit `from` property to be a `string` instead of `string | Promise<string>`
type CallOverrides = Overrides & { from?: string }

type MethodObject = { method: string, debug?: boolean, overrides?: CallOverrides, shouldFail?: boolean }

// This type is after all default values have been applied
type MethodWithDebugAndOverrides = MethodObject & { debug: boolean, overrides: CallOverrides, shouldFail: boolean }

// `test` is used very similarly to `it` from Mocha
export const test = (name, fnNameOrObject, overrides = {}) => it(name, buildTestFn(fnNameOrObject, overrides))
// `test.only` is used very similarly to `it.only` from Mocha, it will isolate all tests marked with `test.only`
test.only = (name, fnNameOrObject, overrides = {}) => it.only(name, buildTestFn(fnNameOrObject, overrides))
// `test.debug` is used to apply `debug: true` to all DSTest contract method calls in the test
test.debug = (name, fnNameOrObject, overrides = {}) => it.only(name, buildTestFn(fnNameOrObject, overrides, true))
// `test.skip` is used very similarly to `it.skip` from Mocha, it will skip all tests marked with `test.skip`
test.skip = (name, fnNameOrObject, overrides = {}) => it.skip(name, buildTestFn(fnNameOrObject, overrides))

// `buildTestFn` is a higher-order function. It returns a function that can be used as the test function for `it`
const buildTestFn = (fnNameOrObject: FnNameOrObject, overrides = {}, debug = false) => {
  // normalize the input to an array of objects
  const fnObjects: MethodWithDebugAndOverrides[] = (Array.isArray(fnNameOrObject) ? fnNameOrObject : [fnNameOrObject]).map(fnNameOrObject => {
    fnNameOrObject = typeof fnNameOrObject === 'string' ? { method: fnNameOrObject } : fnNameOrObject
    // assign all default values and overrides
    fnNameOrObject.overrides = Object.assign({}, overrides, fnNameOrObject.overrides ?? {})
    fnNameOrObject.debug = fnNameOrObject.debug ?? debug
    fnNameOrObject.shouldFail = fnNameOrObject.shouldFail ?? false

    return fnNameOrObject as MethodWithDebugAndOverrides
  })

  // only `step_` prefixed functions can be called on the `DSTest` contracts to clearly separate tests and helpers
  assert(fnObjects.every(({ method }) => method.startsWith('step_')), "Solidity test functions must be prefixed with 'step_'")

  // return the test function that will be used by `it`
  // this function must be defined with the `function` keyword so that `this` is bound to the Mocha context
  return async function () {
    // `Array.prototype.reduce` is used here to ensure that the test functions are called in order.
    // Each test function waits for its predecessor to complete before starting
    return fnObjects.reduce((p: Promise<undefined>, fn) => p.then(async () => {
      const contract = fn.overrides.from
        ? this.testContract.connect(await ethers.getSigner(fn.overrides.from))
        : this.testContract
      const tx = await contract[fn.method](fn.overrides).catch(err => {
        if (fn.shouldFail) {
          if (fn.debug){
             console.error(`smart contract call failed with error:\n${err}\n`)
          }

          return { failed: true }
        }

        console.error("smart contract call failed with error:", err)
        throw err
      })

      // no more assertions necessary if the method-call should fail and did fail
      if (tx.failed && fn.shouldFail) return

      const txReceipt = await tx.wait().catch(err => {
        if (fn.debug) console.error(`tx failed with error:\n${err}\n`)
        return err.receipt
      })

      // `txReceipt.status` will be `0` if the transaction failed.
      // `contract.failed` will return `true` if any of the `DSTest` assertions failed.
      const failed = txReceipt.status === 0 ? true : await contract.failed.staticCall()
      if (fn.debug || failed) {
        console.log('')

        if (!txReceipt.events) console.warn('WARNING: No parseable events found in tx-receipt\n')

        // If `DSTest` assertions failed, the contract will emit logs describing the assertion failure(s).
        txReceipt
          .events
          ?.filter(event => fn.debug || event.event?.startsWith('log'))
          .map(event => event.args?.forEach(arg => console.log(arg)))

        console.log('')
      }

      assert(!failed, `${fn.method} failed`)
    }), Promise.resolve())
  }
}

export const Roles = {
  None: 0,
  Enabled: 1,
  Admin: 2,
  Manager: 3,
}
