package utils

import "encoding/json"

func UnmarshalStr(str string, v any) error {
	return json.Unmarshal(StringToBytes(str), v)
}
