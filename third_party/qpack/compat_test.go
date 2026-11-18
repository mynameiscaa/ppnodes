package qpack

import (
	"bytes"
	"reflect"
	"testing"
)

func TestLegacyDecodeFullCompatibility(t *testing.T) {
	want := []HeaderField{{Name: ":status", Value: "200"}, {Name: "x-ppanel-test", Value: "legacy and current QPACK"}}
	var encoded bytes.Buffer
	encoder := NewEncoder(&encoded)
	for _, field := range want {
		if err := encoder.WriteField(field); err != nil {
			t.Fatal(err)
		}
	}
	decoder := NewDecoder(func(HeaderField) { t.Fatal("DecodeFull must not call the constructor callback") })
	for i := 0; i < 2; i++ {
		got, err := decoder.DecodeFull(encoded.Bytes())
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("DecodeFull = %#v, %v; want %#v", got, err, want)
		}
	}
	if _, err := decoder.DecodeFull([]byte{0xff}); err == nil {
		t.Fatal("malformed QPACK block was accepted")
	}
}
