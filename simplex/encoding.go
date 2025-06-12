//go:generate canoto $GOFILE

package simplex

// CanotoVerifiedBlock is the Canoto representation of a verified block
type CanotoVerifiedBlock struct {
	Metadata   []byte  `canoto:"bytes,1"`
	InnerBlock []byte  `canoto:"bytes,2"`

	canotoData canotoData_CanotoVerifiedBlock
}