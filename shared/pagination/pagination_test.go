package pagination_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/shared/pagination"
)

func TestParams(t *testing.T) {
	p := pagination.Params{Page: 3, Size: 10}
	assert.Equal(t, int32(10), p.Limit())
	assert.Equal(t, int32(20), p.Offset())

	n := pagination.Params{Page: -1, Size: 0}.Normalize()
	assert.Equal(t, pagination.Params{Page: 1, Size: pagination.DefaultSize}, n)

	assert.Equal(t, pagination.MaxSize, pagination.Params{Size: 999}.Normalize().Size)

	huge := pagination.Params{Page: 1 << 40, Size: pagination.MaxSize}
	assert.Positive(t, huge.Offset(), "offset must not overflow int32")
}

func TestNewPage(t *testing.T) {
	page := pagination.NewPage[int](nil, pagination.Params{Page: 2, Size: 10}, 21)
	assert.Equal(t, []int{}, page.Items)
	assert.Equal(t, int64(3), page.TotalPages)

	empty := pagination.NewPage([]int{}, pagination.Params{}, 0)
	assert.Equal(t, int64(0), empty.TotalPages)
}
