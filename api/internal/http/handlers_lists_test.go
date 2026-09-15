package http

import (
	"reflect"
	"testing"
)

// TestListItemRequestEntries covers the flattening of the one-or-many item
// request: the single id first, then the batch, duplicates and blanks dropped,
// order otherwise kept.
func TestListItemRequestEntries(t *testing.T) {
	cases := []struct {
		name string
		in   listItemRequest
		want []string
	}{
		{"single", listItemRequest{EntryID: "a"}, []string{"a"}},
		{"batch", listItemRequest{EntryIDs: []string{"b", "c"}}, []string{"b", "c"}},
		{"both, single first", listItemRequest{EntryID: "a", EntryIDs: []string{"b", "a", "c"}}, []string{"a", "b", "c"}},
		{"blanks and repeats", listItemRequest{EntryIDs: []string{"", "x", "x", ""}}, []string{"x"}},
		{"nothing", listItemRequest{}, []string{}},
	}
	for _, tc := range cases {
		if got := tc.in.entries(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: entries() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
