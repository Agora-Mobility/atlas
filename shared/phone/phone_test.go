package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/shared/phone"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "e164", raw: "+244923000000", want: "+244923000000"},
		{name: "national", raw: "923000000", want: "+244923000000"},
		{name: "national with spaces", raw: " 923 000 000 ", want: "+244923000000"},
		{name: "international with spaces", raw: "+244 923 000 000", want: "+244923000000"},
		{name: "country code without plus", raw: "244923000000", want: "+244923000000"},
		{name: "international dialing prefix", raw: "00244923000000", want: "+244923000000"},
		{name: "dashes and parentheses", raw: "(+244) 923-000-000", want: "+244923000000"},
		{name: "other country", raw: "+351 912 345 678", want: "+351912345678"},
		{name: "invalid is only trimmed", raw: " 12345 ", want: "12345"},
		{name: "letters are only trimmed", raw: "abc", want: "abc"},
		{name: "empty", raw: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, phone.Normalize(tc.raw))
		})
	}
}

func TestIsValid(t *testing.T) {
	assert.True(t, phone.IsValid("+244923000000"))
	assert.True(t, phone.IsValid("+351912345678"))
	assert.False(t, phone.IsValid("923000000"))
	assert.False(t, phone.IsValid("+244 923 000 000"))
	assert.False(t, phone.IsValid("+24412345"))
	assert.False(t, phone.IsValid(""))
}
