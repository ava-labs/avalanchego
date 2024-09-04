package merkledb

import "context"

var _ ReadOnlyTrie = (*snapshot)(nil)

type snapshot struct {
	revisionsLeft   int
	innerView       *view
	innermostParent *view
}

func (s *snapshot) GetValue(ctx context.Context, key []byte) ([]byte, error) {
	return s.innerView.GetValue(ctx, key)
}

func (s *snapshot) GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error) {
	return s.innerView.GetValues(ctx, keys)
}

// maskingChanges applies the changes in the changeSummary in reverse so that the snapshot stays consistent even though the underlying db has changed
func (s *snapshot) updateParent(v *view) {
	s.revisionsLeft--
	if s.revisionsLeft == 0 {
		s.innermostParent.invalidate()
		return
	}
	s.innermostParent.updateParent(v)
	s.innermostParent = v
}
