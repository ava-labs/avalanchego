package platformvm

import "testing"

func TestBaseTxSyntacticVerify(t *testing.T) {
	tx := BaseTx{}
	if err := tx.SyntacticVerify(); err != nil {
		t.Fatalf("should have passed verification because len(memo) <= maxMemoSize but got %s", err)
	}
	tx.Memo = make([]byte, maxMemoSize)
	if err := tx.SyntacticVerify(); err != nil {
		t.Fatalf("should have passed verification because len(memo) <= maxMemoSize but got %s", err)
	}
	tx.Memo = append(tx.Memo, byte(0))
	if err := tx.SyntacticVerify(); err == nil {
		t.Fatal("should have failed verification because len(memo) > maxMemoSize")
	}
}
