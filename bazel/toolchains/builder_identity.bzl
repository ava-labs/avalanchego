"""Declare the container-provided compiler environment as a toolchain input."""

load("@bazel_skylib//rules:common_settings.bzl", "BuildSettingInfo")

def _builder_identity_impl(ctx):
    identity = ctx.actions.declare_file(ctx.label.name + ".txt")
    ctx.actions.write(identity, ctx.attr.digest[BuildSettingInfo].value + "\n")
    return [DefaultInfo(files = depset([identity]))]

builder_identity = rule(
    implementation = _builder_identity_impl,
    attrs = {
        "digest": attr.label(mandatory = True, providers = [BuildSettingInfo]),
    },
)
