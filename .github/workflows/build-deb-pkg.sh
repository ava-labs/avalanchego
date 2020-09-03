PKG_ROOT=/tmp/avalanche
DEBIAN_BASE_DIR=$PKG_ROOT/debian
AVALANCHE_BUILD_BIN_DIR=$DEBIAN_BASE_DIR/usr/local/bin
AVALANCHE_LIB_DIR=$DEBIAN_BASE_DIR/usr/local/lib/avalanche
TEMPLATE=.github/workflows/debian/template 
DEBIAN_CONF=$DEBIAN_BASE_DIR/DEBIAN

mkdir -p $DEBIAN_BASE_DIR
mkdir -p $DEBIAN_CONF
mkdir -p $AVALANCHE_BUILD_BIN_DIR
mkdir -p $AVALANCHE_LIB_DIR

OK=`cp ./build/avalanche $AVALANCHE_BUILD_BIN_DIR`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi
OK=`cp ./build/plugins/evm $AVALANCHE_LIB_DIR`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi
OK=`cp $TEMPLATE/control $DEBIAN_CONF`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi

echo "Build debian package..."
cd $PKG_ROOT
echo "Tag: $TAG"
NEW_VERSION_STRING="Version: $TAG"
sed -i "s/Version.*/$NEW_VERSION_STRING/g" debian/DEBIAN/control
dpkg-deb --build debian avalanche-linux_$TAG.deb
aws s3 cp avalanche-linux_$TAG.deb s3://$BUCKET/linux/
