package utils

import (
	"github.com/jfrog/build-info-go/utils/compareutils"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEqualSlices(t *testing.T) {
	data := []struct {
		a, b []string
		res  bool
	}{
		{[]string{"1", "2", "3"}, []string{"1", "2", "3"}, true},
		{[]string{"1", "3", "2"}, []string{"1", "2", "3"}, true},
		{[]string{"1", "3", "2"}, []string{"3", "2", "1"}, true},
		{[]string{"1", "3"}, []string{"1", "2", "3"}, false},
		{[]string{"1", "2", "2", "3"}, []string{"1", "2", "3"}, false},
		{[]string{"1", "2", "3"}, []string{"1", "2", "2", "3"}, false},
		{[]string{"1", "(2)", "3"}, []string{"1", "2", "3"}, false},
		{[]string{}, []string{"1", "2", "3"}, false},
		{[]string{}, []string{}, true},
		{nil, []string{}, true},
		{nil, nil, true},
	}
	for _, d := range data {
		if got := compareutils.IsEqualSlices(d.a, d.b); got != d.res {
			t.Errorf("IsEqualSlices(%v, %v) == %v, want %v", d.a, d.b, got, d.res)
		}
	}
}

func TestIsEqual2DSlices(t *testing.T) {
	data := []struct {
		a, b [][]string
		res  bool
	}{
		{[][]string{{"1", "2", "3"}}, [][]string{{"1", "2", "3"}}, true},
		{[][]string{{"1", "2", "3"}}, [][]string{{"4", "5", "6"}}, false},
		{[][]string{{"1", "2"}}, [][]string{{"1", "2", "3"}}, false},
		{[][]string{{"1", "2", "3"}}, [][]string{{"1", "2"}}, false},
		{[][]string{{"1", "2", "3"}, {"1", "2", "3"}}, [][]string{{"1", "2", "3"}}, false},
		{[][]string{{"1", "2", "3"}, {}}, [][]string{{"1", "2", "3"}}, false},
		{[][]string{{}}, [][]string{{"1", "2", "3"}}, false},
		{[][]string{{}}, [][]string{{}}, true},
	}
	for _, d := range data {
		if got := compareutils.IsEqual2DSlices(d.a, d.b); got != d.res {
			t.Errorf("IsEqual2DSlices(%v, %v) == %v, want %v", d.a, d.b, got, d.res)
		}
	}
}

func TestTo1DSlice(t *testing.T) {
	data := []struct {
		a   [][]string
		res []string
	}{
		{[][]string{{"1", "2", "3"}}, []string{"123"}},
		{[][]string{{"1"}}, []string{"1"}},
		{[][]string{{}}, []string{""}},
	}
	for _, d := range data {
		if got := compareutils.To1DSlice(d.a); !assert.ElementsMatch(t, got, d.res) {
			t.Errorf("to1DSlice(%v) == %v, want %v", d.a, got, d.res)
		}
	}
}
