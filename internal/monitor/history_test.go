package monitor

import (
	"reflect"
	"testing"
)

func TestHistoryWraps(t *testing.T) {
	history := NewHistory(3)
	for _, value := range []float64{1, 2, 3, 4} {
		history.Add(value)
	}
	if got, want := history.Values(), []float64{2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Values() = %v, want %v", got, want)
	}
}

func TestHistoryResizeRetainsNewest(t *testing.T) {
	history := NewHistory(4)
	for _, value := range []float64{1, 2, 3, 4} {
		history.Add(value)
	}
	history.Resize(2)
	if got, want := history.Values(), []float64{3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after shrink Values() = %v, want %v", got, want)
	}
	history.Resize(5)
	history.Add(5)
	if got, want := history.Values(), []float64{3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after grow Values() = %v, want %v", got, want)
	}
}

func TestHistoryZeroCapacity(t *testing.T) {
	history := NewHistory(0)
	history.Add(1)
	if got := history.Values(); len(got) != 0 {
		t.Fatalf("Values() = %v, want empty", got)
	}
	history.Resize(0)
}
