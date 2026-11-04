package settleledger

// SetModes switches the plant settings for tests.
func SetModes(presize, v2, bulk bool) { batchPresize, keyspaceV2, bulkSlots = presize, v2, bulk }
