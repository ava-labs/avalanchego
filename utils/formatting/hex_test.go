package formatting

import (
	"bytes"
	"testing"
)

func TestHexEncoding(t *testing.T) {
	rawBytes := []byte{1, 2, 3, 4}
	h1 := HexWrapper{Bytes: rawBytes}
	str := h1.String()

	h2 := HexWrapper{}
	if err := h2.FromString(str); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(h2.Bytes, h1.Bytes) {
		t.Fatal("HexWrapper produced different bytes")
	}

	json, err := h1.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	h3 := HexWrapper{}
	if err := h3.UnmarshalJSON(json); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(h1.Bytes, h3.Bytes) {
		t.Fatal("Unmarshal HexWrapper did not reverse Marshal HexWrapper")
	}
}
