package tier0

import (
	"sort"
)

// Reads of the open tier0 (the blocks Add took since the last cut) for a process that serves
// them before the cut (cmd/node's RPC). Each gives what the method of the same name of the cut
// file gives for the same block, read from the writer's spools and bodies store on disk (no
// copy in RAM). Not safe concurrently with Add, Cut or Close: the caller serializes them (when
// Add cuts, the blocks leave the writer: read them from the cut file).

// FirstOrdinal is the chain-wide ordinal of the open tier0's first tx.
func (w *Writer) FirstOrdinal() uint64 { return w.ordinal }

// Body is open block i's body.
func (w *Writer) Body(i int) ([]byte, error) { return w.Record(SecBodies, i) }

// Wrapper is open block i's wrapper, nil when the block has none.
func (w *Writer) Wrapper(i int) (*Wrapper, error) { return wrapper(w.Record(SecWrappers, i)) }

// Container is open block i's container bytes.
func (w *Writer) Container(i int) ([]byte, error) { return container(w.Body(i))(w.Wrapper(i)) }

// Receipts is open block i's receipts in consensus encoding.
func (w *Writer) Receipts(i int) ([][]byte, error) { return receipts(w.Record(SecReceipts, i)) }

// Outputs is open block i's tx outputs.
func (w *Writer) Outputs(i int) ([][]byte, error) { return listOf(w.Record(SecOutputs, i)) }

// Codes is the code of every code hash open block i's writes set, sorted by code hash.
func (w *Writer) Codes(i int) ([][]byte, error) { return listOf(w.Record(SecCodes, i)) }

// Writes is open block i's write sets.
func (w *Writer) Writes(i int) ([][]Write, error) { return writes(w.Record(SecWrites, i)) }

// Ordinal is the chain-wide ordinal of open block i's first tx.
func (w *Writer) Ordinal(i int) uint64 { return w.ordinal + uint64(w.cum[i]) }

// Locate finds the open block and position of the tx with the given chain-wide ordinal.
func (w *Writer) Locate(ordinal uint64) (block, pos int, ok bool) {
	if ordinal < w.ordinal || ordinal-w.ordinal >= uint64(w.txs) {
		return 0, 0, false
	}
	o := uint32(ordinal - w.ordinal)
	block = sort.Search(w.n, func(i int) bool {
		next := uint32(w.txs)
		if i+1 < w.n {
			next = w.cum[i+1]
		}
		return next > o
	})
	return block, int(o - w.cum[block]), true
}
