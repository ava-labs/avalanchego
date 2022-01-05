PKG_ROOT=/tmp
VERSION=$TAG
AVALANCHE_ROOT=$PKG_ROOT/avalanchego-$VERSION

mkdir -p $AVALANCHE_ROOT

OK=`cp ./build/avalanchego $AVALANCHE_ROOT`
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
tar -czvf "avalanchego-linux-$ARCH-$VERSION.tar.gz" avalanchego-$VERSION
aws s3 cp avalanchego-linux-$ARCH-$VERSION.tar.gz s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/
rm -rf $PKG_ROOT/avalanchego*
