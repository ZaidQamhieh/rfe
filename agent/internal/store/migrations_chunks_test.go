package store

import (
	"reflect"
	"testing"
)

func TestParseLegacyChunks(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []int
		wantErr bool
	}{
		{"intact list", "[0,1,2]", []int{0, 1, 2}, false},
		{"empty list", "[]", nil, false},
		{"cut short keeps the intact numbers and drops the last", "[0,1,2,3", []int{0, 1, 2}, true},
		{"cut after a comma", "[0,1,", []int{0}, true},
		{"nothing readable", "[", nil, true},
		{"not a list", `{"a":1}`, nil, true},
		{"junk after numbers", "[0,1,x]", []int{0}, true},
	}
	for _, c := range cases {
		got, err := parseLegacyChunks(c.raw)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
		}
		if !reflect.DeepEqual(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
