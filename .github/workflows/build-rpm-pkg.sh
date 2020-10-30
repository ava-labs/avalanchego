PKG_ROOT=/tmp/avalanchego
RPM_BASE_DIR=$PKG_ROOT/yum
AVALANCHE_BUILD_BIN_DIR=$RPM_BASE_DIR/usr/local/bin
AVALANCHE_LIB_DIR=$RPM_BASE_DIR/usr/local/lib/avalanchego

mkdir -p $RPM_BASE_DIR
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
VER=$(echo $TAG | gawk -F- '{print$1}' | tr -d 'v' )
REL=$(echo $TAG | gawk -F- '{print$2}')
[ -z "$REL" ] && REL=0 
echo "Tag: $VER"
rpmbuild --bb --define "version $VER" --define "release $REL" --buildroot $RPM_BASE_DIR .github/workflows/yum/specfile/avalanchego.spec
aws s3 cp ~/rpmbuild/RPMS/x86_64/avalanchego-*.rpm s3://$BUCKET/linux/rpm/
