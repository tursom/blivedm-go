package utils

import "encoding/base64"

// StringToBytes 返回独立的可写副本，避免修改字符串底层内存。
func StringToBytes(s string) []byte {
	if s == "" {
		return nil
	}
	return []byte(s)
}

// BytesToString 返回不受后续切片修改影响的字符串。
func BytesToString(b []byte) string { return string(b) }

func B64Decode(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
