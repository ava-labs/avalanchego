PKG_ROOT=/tmp/avalanchego
RPM_BASE_DIR=$PKG_ROOT/yum
AVALANCHE_BUILD_BIN_DIR=$RPM_BASE_DIR/usr/local/bin
AVALANCHE_LIB_DIR=$RPM_BASE_DIR/usr/local/lib/avalanchego

mkdir -p $DEBIAN_BASE_DIR
mkdir -p $AVALANCHE_BUILD_BIN_DIR
mkdir -p $AVALANCHE_LIB_DIR

OK=`cp ./build/avalanchego $AVALANCHE_BUILD_BIN_DIR`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi
OK=`cp ./build/plugins/evm $AVALANCHE_LIB_DIR`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi

echo "Build rpm package..."
echo "Tag: $TAG"
VER=$TAG
if [[ $TAG =~ ^v ]]; then
  VER=$(echo $TAG | cut -d'v' -f 2)
fi
NEW_VERSION_STRING="Version: $VER"
sed -i "s/Version.*/$NEW_VERSION_STRING/g" yum/specfile/avalanchego.spec
rpmbuild --bb --buildroot $RPM_BASE_DIR .github/workflows/yum/specfile/avalanchego.spec
aws s3 cp ~/rpmbuild/RPMS/avalanchego-*.rpm s3://$BUCKET/linux/
