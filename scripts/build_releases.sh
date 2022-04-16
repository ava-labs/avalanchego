#!/usr/bin/env bash

if [ -z "$GITHUB_TOKEN" ]; then
	GITHUB_TOKEN=""
fi

set -o errexit
set -o nounset
set -o pipefail

RELEASE_ID="$(git describe --tag)"
# caminogo root folder
CAMINO_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
# Authentication
AUTH_HEADER=""
# check if GITHUB_TOKEN is set
if [ -n "$GITHUB_TOKEN" ]; then
	AUTH_HEADER="Authorization: token ${GITHUB_TOKEN}"
fi

publish () {
	# check if GITHUB_TOKEN is set
	if [ -z "$AUTH_HEADER" ]; then
		return 0
	fi
	FILENAME=$(basename "$1")
	UPLOAD_URL="https://uploads.github.com/repos/${GITHUB_REPOSITORY}/releases/${RELEASE_ID}/assets?name=${FILENAME}"

	# create a temp file for upload output
	logOut=$(mktemp)
	# Upload the artifact - capturing HTTP response-code in our output file.
	response=$(curl \
		-sSL \
		-XPOST \
		-H "${AUTH_HEADER}" \
		--upload-file "$1" \
		--header "Content-Type:application/octet-stream" \
		--write-out "%{http_code}" \
		--output $logOut \
		"${UPLOAD_URL}")

	cat $logOut
	rm $logOut

	if [ "$?" -ne 0 ]; then
		echo "err: curl command failed!!!"
		return 1
	fi

	if [ $response -ge 400 ]; then
		echo "err: upload not successful ($response)!!!"
 		return 1
	fi
}

echo "Building release OS=linux and ARCH=amd64 using GOAMD64 V2 for caminogo version $RELEASE_ID"
rm -rf $CAMINO_PATH/build/*

# build executables into build dir
GOOS=linux GOARCH=amd64 GOAMD64=v2 $CAMINO_PATH/scripts/build.sh
# copy the license file
cp $CAMINO_PATH/LICENSE $CAMINO_PATH/build
# prepare a fresh dist folder
rm -rf $CAMINO_PATH/dist && mkdir $CAMINO_PATH/dist
# create the package
echo "building artifact"
ARTIFACT=$CAMINO_PATH/dist/caminogo-linux-amd64-$RELEASE_ID.tar.gz
tar -czf $ARTIFACT -C $CAMINO_PATH/build/ .
# publish the newly generated file
publish $ARTIFACT
