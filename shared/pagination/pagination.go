// Package pagination provides offset pagination parameters and a generic page.
package pagination

const (
	DefaultPage = 1
	DefaultSize = 20
	MaxSize     = 100
)

type Params struct {
	Page int `form:"page" json:"page"`
	Size int `form:"size" json:"size"`
}

// Normalize clamps the parameters to valid values.
func (p Params) Normalize() Params {
	if p.Page < 1 {
		p.Page = DefaultPage
	}
	if p.Size < 1 {
		p.Size = DefaultSize
	}
	if p.Size > MaxSize {
		p.Size = MaxSize
	}
	return p
}

func (p Params) Limit() int32 {
	return int32(p.Normalize().Size) //nolint:gosec // bounded by MaxSize
}

func (p Params) Offset() int32 {
	n := p.Normalize()
	return int32((n.Page - 1) * n.Size) //nolint:gosec // bounded in practice by Page * MaxSize
}

type Page[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	Total      int64 `json:"total"`
	TotalPages int64 `json:"total_pages"`
}

func NewPage[T any](items []T, p Params, total int64) Page[T] {
	n := p.Normalize()
	if items == nil {
		items = []T{}
	}
	pages := total / int64(n.Size)
	if total%int64(n.Size) != 0 {
		pages++
	}
	return Page[T]{Items: items, Page: n.Page, Size: n.Size, Total: total, TotalPages: pages}
}
