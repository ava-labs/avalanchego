# ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
# This script is intended to be wrapped with a thin script passing in testsuite-specific
#  variables (e.g. the name of the testsuite image)
# ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
set -euo pipefail

# ====================== CONSTANTS =======================================================
BUILD_ACTION="build"
RUN_ACTION="run"
BOTH_ACTION="all"
HELP_ACTION="help"

# ====================== ARG PARSING =======================================================
show_help_and_exit() {
    echo "$(basename ${0}) action suite_image_name repo_dirpath dockerfile_filepath [run_arg1] [run_arg2]..."
    echo ""
    echo "  This script will optionally a) build your Kurtosis testsuite into a Docker image and/or b) run it via a call to the kurtosis.sh script"
    echo ""
    echo "    action                Determines the action this script will execute ('${HELP_ACTION}' to print this message, '${BUILD_ACTION}' to only build the testsuite"
    echo "                              image, '${RUN_ACTION}' to just run the testsuite image, and '${BOTH_ACTION}' to both build and run the testsuite)"
    echo "    suite_image           The name to give the built Docker image containing the testsuite"
    echo "    repo_dirpath          Path to the root of the repo containing the testsuite to build"
    echo "    dockerfile_filepath   Filepath to the Dockerfile for building the testsuite Docker image"
    echo "    wrapper_filepath      Filepath to the kurtosis.sh wrapper script"
    echo "    run_arg               Argument to pass to kurtosis.sh when running the testsuite (use '--help' here to see all options)"
    echo ""
    echo "  To see the args the kurtosis.sh script accepts for the 'run' phase, call '$(basename ${0}) all --help'"
    echo ""
    exit 1     # Exit with error code so we dont't get spurious CI passes
}

if [ "${#}" -lt 5 ]; then
    show_help_and_exit
fi

action="${1:-}"
shift 1

suite_image="${1:-}"
shift 1

repo_dirpath="${1:-}"
shift 1

dockerfile_filepath="${1:-}"
shift 1

wrapper_filepath="${1:-}"
shift 1

do_build=true
do_run=true
case "${action}" in
    ${HELP_ACTION})
        show_help_and_exit
        ;;
    ${BUILD_ACTION})
        do_build=true
        do_run=false
        ;;
    ${RUN_ACTION})
        do_build=false
        do_run=true
        ;;
    ${BOTH_ACTION})
        do_build=true
        do_run=true
        ;;
    *)
        echo "Error: Action argument must be one of '${HELP_ACTION}', '${BUILD_ACTION}', '${RUN_ACTION}', or '${BOTH_ACTION}'" >&2
        exit 1
        ;;
esac

# ====================== ARG VALIDATION ===================================================
if [ -z "${suite_image}" ]; then
    echo "Error: Suite image name cannot be empty" >&2
    exit 1
fi
if [ -z "${repo_dirpath}" ]; then
    echo "Error: Repo dirpath cannot be empty" >&2
    exit 1
fi
if ! [ -d "${repo_dirpath}" ]; then
    echo "Error: Repo dirpath '${repo_dirpath}' is not a directory" >&2
    exit 1
fi
if [ -z "${dockerfile_filepath}" ]; then
    echo "Error: Dockerfile filepath cannot be empty" >&2
    exit 1
fi
if ! [ -f "${dockerfile_filepath}" ]; then
    echo "Error: Dockerfile filepath '${dockerfile_filepath}' is not a file" >&2
    exit 1
fi
if [ -z "${wrapper_filepath}" ]; then
    echo "Error: Wrapper filepath cannot be empty" >&2
    exit 1
fi
if ! [ -f "${wrapper_filepath}" ]; then
    echo "Error: Wrapper filepath '${dockerfile_filepath}' is not a file" >&2
    exit 1
fi

# ====================== MAIN LOGIC =======================================================
# Captures the first of tag > branch > commit
git_ref="$(cd "${repo_dirpath}" && git describe --tags --exact-match 2> /dev/null || git symbolic-ref -q --short HEAD || git rev-parse --short HEAD)"
if [ "${git_ref}" == "" ]; then
    echo "Error: Could not determine a Git ref to use for the Docker tag; is the repo a Git directory?" >&2
    exit 1
fi

docker_tag="$(echo "${git_ref}" | sed 's,[/:],_,g')"
if [ "${docker_tag}" == "" ]; then
    echo "Error: Sanitizing Git ref to get a Docker tag for the testsuite yielded an empty string" >&2
    exit 1
fi

if "${do_build}"; then
    if ! [ -f "${repo_dirpath}"/.dockerignore ]; then
        echo "Error: No .dockerignore file found in root of repo '${repo_dirpath}'; this is required so Docker caching is enabled and your testsuite builds remain quick" >&2
        exit 1
    fi

    echo "Building '${suite_image}' Docker image..."
    if ! docker build -t "${suite_image}:${docker_tag}" -f "${dockerfile_filepath}" "${repo_dirpath}"; then
        echo "Error: Docker build of the testsuite failed" >&2
        exit 1
    fi
fi

if "${do_run}"; then
    # The funky ${1+"${@}"} incantation is how you you feed arguments exactly as-is to a child script in Bash
    # ${*} loses quoting and ${@} trips set -e if no arguments are passed, so this incantation says, "if and only if 
    #  ${1} exists, evaluate ${@}"
    if ! bash "${wrapper_filepath}" ${1+"${@}"} "${suite_image}:${docker_tag}"; then
        echo "Error: Testsuite execution failed"
        exit 1
    fi
fi
