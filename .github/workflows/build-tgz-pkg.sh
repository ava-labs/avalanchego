PKG_ROOT=/tmp
VERSION=$TAG
AVALANCHE_ROOT=$PKG_ROOT/avalanche-$VERSION

mkdir -p $AVALANCHE_ROOT

OK=`cp ./build/avalanche $AVALANCHE_ROOT`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi
OK=`cp -r ./build/plugins $AVALANCHE_ROOT`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi

echo "Build tgz package..."
cd $PKG_ROOT
echo "Version: $VERSION"
tar -czvf "avalanche-linux-$VERSION.tar.gz" avalanche-$VERSION
aws s3 cp avalanche-linux-$VERSION.tar.gz s3://avalanche-public-builds/linux/
