//go:build inject

package tier0

// SetCutTxs makes writers cut at n txs instead of CutTxs. TEST ONLY (build tag inject): the
// files differ from every other node's.
func SetCutTxs(n int) { cutTxs = n }
