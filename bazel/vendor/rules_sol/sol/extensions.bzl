"""Module extension that materialises solc toolchains under bzlmod.

Replaces the `sol_register_toolchains(...)` calls previously made from WORKSPACE.
"""

load("//sol:repositories.bzl", "sol_repositories")
load("//sol/private:toolchains_repo.bzl", "PLATFORMS", "toolchains_repo")

_VERSIONS = [
    "0.7.6",
    "0.8.9",
    "0.8.18",
    "0.8.19",
    "0.8.24",
]

def _sol_toolchains_impl(_module_ctx):
    for v in _VERSIONS:
        name = "solc_" + v.replace(".", "_")
        for platform in PLATFORMS.keys():
            sol_repositories(
                name = name + "_" + platform,
                platform = platform,
                sol_version = v,
            )
        toolchains_repo(
            name = name + "_toolchains",
            user_repository_name = name,
            version = v,
        )

sol_toolchains = module_extension(
    implementation = _sol_toolchains_impl,
)
