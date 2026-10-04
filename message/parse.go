package message

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/tidwall/gjson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// decodeData validates the envelope before decoding. A temporary value prevents
// malformed messages from partially changing a previously parsed receiver.
func decodeData[T any](body []byte, dst *T) error {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode notification: %w", err)
	}
	data := bytes.TrimSpace(envelope.Data)
	if len(data) == 0 || data[0] != '{' {
		return errors.New("notification data must be an object")
	}
	var next T
	if err := json.Unmarshal(data, &next); err != nil {
		return fmt.Errorf("decode notification data: %w", err)
	}
	*dst = next
	return nil
}

func decodeProto(body []byte, dst proto.Message) error {
	var data struct {
		PB *string `json:"pb"`
	}
	if err := decodeData(body, &data); err != nil {
		return err
	}
	if data.PB == nil {
		return errors.New("notification has no protobuf payload")
	}
	return decodeBase64Proto(*data.PB, dst)
}

func decodeBase64Proto(encoded string, dst proto.Message) error {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode protobuf base64: %w", err)
	}
	if err := validateProtoIntegers(data, dst.ProtoReflect().Descriptor(), 0); err != nil {
		return fmt.Errorf("decode protobuf: %w", err)
	}
	if err := (proto.UnmarshalOptions{RecursionLimit: 100}).Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode protobuf: %w", err)
	}
	return nil
}

func checkedInt(value uint64) (int, error) {
	if value > uint64(math.MaxInt) {
		return 0, fmt.Errorf("integer %d exceeds int range", value)
	}
	return int(value), nil
}

func setInts(pairs ...intField) error {
	for _, pair := range pairs {
		value, err := checkedInt(pair.value)
		if err != nil {
			return err
		}
		*pair.dst = value
	}
	return nil
}

type intField struct {
	dst   *int
	value uint64
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// normalizeIntFlags accepts both wire representations used by Bilibili for
// fields whose historical public Go type is int. It never mutates the input.
func normalizeIntFlags(data []byte, paths ...string) []byte {
	for _, path := range paths {
		value := gjson.GetBytes(data, path)
		if value.Type != gjson.True && value.Type != gjson.False {
			continue
		}
		start, end := value.Index, value.Index+len(value.Raw)
		if start < 0 || end > len(data) {
			continue
		}
		replacement := byte('0')
		if value.Type == gjson.True {
			replacement = '1'
		}
		next := make([]byte, 0, len(data)-len(value.Raw)+1)
		next = append(next, data[:start]...)
		next = append(next, replacement)
		next = append(next, data[end:]...)
		data = next
	}
	return data
}

func requireJSONFields(data []byte, paths ...string) error {
	for _, path := range paths {
		value := gjson.GetBytes(data, path)
		if !value.Exists() || value.Type == gjson.Null {
			return fmt.Errorf("missing required field %s", path)
		}
	}
	return nil
}

func normalizeBoolFlags(data []byte, paths ...string) []byte {
	for _, path := range paths {
		value := gjson.GetBytes(data, path)
		if value.Type != gjson.Number || (value.Raw != "0" && value.Raw != "1") {
			continue
		}
		start, end := value.Index, value.Index+len(value.Raw)
		if start < 0 || end > len(data) {
			continue
		}
		replacement := "false"
		if value.Raw == "1" {
			replacement = "true"
		}
		next := make([]byte, 0, len(data)+len(replacement)-1)
		next = append(next, data[:start]...)
		next = append(next, replacement...)
		next = append(next, data[end:]...)
		data = next
	}
	return data
}

// protobuf's uint32 decoder truncates oversized varints. Reject those values
// before unmarshalling, matching the reference protocol's checked conversions.
func validateProtoIntegers(data []byte, descriptor protoreflect.MessageDescriptor, depth int) error {
	if depth >= 100 {
		return fmt.Errorf("protobuf nesting exceeds limit")
	}
	for len(data) != 0 {
		number, wireType, tagLen := protowire.ConsumeTag(data)
		if tagLen < 0 {
			return protowire.ParseError(tagLen)
		}
		data = data[tagLen:]
		length := protowire.ConsumeFieldValue(number, wireType, data)
		if length < 0 {
			return protowire.ParseError(length)
		}
		field := descriptor.Fields().ByNumber(number)
		if field != nil {
			switch {
			case field.Kind() == protoreflect.Uint32Kind && wireType == protowire.VarintType:
				value, _ := protowire.ConsumeVarint(data)
				if value > math.MaxUint32 {
					return fmt.Errorf("%s exceeds uint32 range", field.FullName())
				}
			case field.Kind() == protoreflect.MessageKind && wireType == protowire.BytesType:
				value, _ := protowire.ConsumeBytes(data)
				if err := validateProtoIntegers(value, field.Message(), depth+1); err != nil {
					return err
				}
			}
		}
		data = data[length:]
	}
	return nil
}
