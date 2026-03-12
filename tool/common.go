package tool

func NewPtr[T any](v T) *T {
	return &v
}
