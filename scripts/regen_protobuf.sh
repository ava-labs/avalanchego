
for pbfile in $(find . | grep '\.proto$')
do
  PBFILE=$(echo $pbfile | rev | cut -d '/' -f 1 | rev)
  PBPATH=$(echo $pbfile | rev | cut -d '/' -f 2- | rev)
  PBPKG=$(echo $PBPATH | rev | cut -d '/' -f 1 | rev)
  protoc -I=$PBPATH --go_out=$PBPATH --go_opt=paths=source_relative --go_opt M$PBFILE=/$PBPKG $PBPATH/$PBFILE
done
