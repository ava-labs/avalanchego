"""Macros for generated unit-test shard suites."""

load("//.bazel:generated_test_suites.bzl", "UNIT_TEST_SHARDS")

def unit_test_suites():
    """Declare root-level test_suite targets for generated unit-test shards."""
    for name, tests in UNIT_TEST_SHARDS.items():
        native.test_suite(
            name = name,
            tests = tests,
        )

    native.test_suite(
        name = "unit_tests",
        tests = [":%s" % name for name in UNIT_TEST_SHARDS],
    )
