package utils

import "testing"

func TestConversionsDoNotAlias(t *testing.T) {
	original := "immutable"
	bytes := StringToBytes(original)
	bytes[0] = 'I'
	if original != "immutable" {
		t.Fatal("StringToBytes returned mutable string storage")
	}
	converted := BytesToString(bytes)
	bytes[0] = 'x'
	if converted != "Immutable" {
		t.Fatal("BytesToString aliases mutable storage")
	}
}

func TestB64Decode(t *testing.T) {
	decoded, err := B64Decode("5by55bmV")
	if err != nil || string(decoded) != "弹幕" {
		t.Fatalf("B64Decode = %q, %v", decoded, err)
	}
	if _, err := B64Decode("invalid!"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
}
