PKG_ROOT=/tmp
VERSION=$TAG
CAMINO_ROOT=$PKG_ROOT/caminogo-$VERSION

mkdir -p $CAMINO_ROOT

OK=`cp ./build/caminogo $CAMINO_ROOT`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi
OK=`cp -r ./build/plugins $CAMINO_ROOT`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi


echo "Build tgz package..."
cd $PKG_ROOT
echo "Version: $VERSION"
tar -czvf "caminogo-linux-$ARCH-$VERSION.tar.gz" caminogo-$VERSION
aws s3 cp caminogo-linux-$ARCH-$VERSION.tar.gz s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/
rm -rf $PKG_ROOT/caminogo*
