package tool

import "encoding/json"

func NewPtr[T any](v T) *T {
	return &v
}

func BytesReplace(src []byte, old, new uint8) {
	for i, b := range src {
		if b == old {
			src[i] = new
		}
	}
}

func ToJson(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "json.Marshal error:" + err.Error()
	}
	return string(b)
}
