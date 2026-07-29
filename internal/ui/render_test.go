package ui

import (
	"reflect"
	"testing"
)

func TestTailLinesKeepsNewestCompleteLines(t *testing.T) {
	got := tailLines([]string{"one", "two", "three"}, 2)
	if !reflect.DeepEqual(got, []string{"two", "three"}) {
		t.Fatalf("tail = %#v", got)
	}
}
