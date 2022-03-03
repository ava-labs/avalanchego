// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethdb

// CappedBatch implements ethdb.Batch and calls Write
// whenever the underlying ValueSize is greater than
// commitSize.
// [commitFn] will be called when the batch is written
// if provided.
// This is used to commit to an underlying versiondb.
// NOTE: Put and Delete operations written to this
// batch will be written to disk when [batch.ValueSize]
// reaches [commitSize]. It should not be assumed the
// entire batch will be committed atomically.
type CappedBatch struct {
	Batch
	commitSize int
	commitFn   func() error
}

var _ Batch = &CappedBatch{}

func NewCappedBatch(batch Batch, commitSize int, commitFn func() error) *CappedBatch {
	return &CappedBatch{
		Batch:      batch,
		commitSize: commitSize,
		commitFn:   commitFn,
	}
}

func (c *CappedBatch) Put(key, value []byte) error {
	if err := c.Batch.Put(key, value); err != nil {
		return err
	}
	return c.cap()
}

func (c *CappedBatch) Delete(key []byte) error {
	if err := c.Batch.Delete(key); err != nil {
		return err
	}
	return c.cap()
}

func (c *CappedBatch) cap() error {
	if c.Batch.ValueSize() <= c.commitSize {
		return nil
	}
	return c.write()
}

func (c *CappedBatch) write() error {
	if err := c.Batch.Write(); err != nil {
		return err
	}
	c.Batch.Reset()
	if c.commitFn != nil {
		return c.commitFn()
	}
	return nil
}

func (c *CappedBatch) Write() error {
	if c.Batch.ValueSize() == 0 {
		return nil
	}
	return c.write()
}
