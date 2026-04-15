"Shows how to write your own macro which uses the solidity compiler"

def my_sol_macro(name, srcs, solc_version):
    v = solc_version.replace(".", "_")
    native.genrule(
        name = name,
        srcs = srcs,
        outs = [s + "_json.ast" for s in srcs],
        cmd = " ".join([
            "$(SOLC_BIN)",
            "--ast-compact-json",
            "-o",
            "$(RULEDIR)",
        ] + ["$(locations {})".format(s) for s in srcs]),
        toolchains = ["@solc_{}_toolchains//:resolved_toolchain".format(v)],
        tools = ["@solc_{}_toolchains//:resolved_toolchain".format(v)],
    )
