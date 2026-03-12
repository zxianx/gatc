package tool

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
