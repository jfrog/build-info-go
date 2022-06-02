package compareutils

import (
	"reflect"
	"sort"
)

// Compare two slices irrespective of elements order.
func IsEqualSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	a_copy := make([]string, len(a))
	b_copy := make([]string, len(b))

	copy(a_copy, a)
	copy(b_copy, b)

	sort.Strings(a_copy)
	sort.Strings(b_copy)

	return reflect.DeepEqual(a_copy, b_copy)
}

// Compare two dimensional slices irrespective of elements order.
func IsEqual2DSlices(a, b [][]string) bool {
	return IsEqualSlices(To1DSlice(a), To1DSlice(b))
}

// Transform two dimensional slice to one dimensional slice
func To1DSlice(a [][]string) (result []string) {
	for _, i := range a {
		temp := ""
		for _, j := range i {
			temp += j
		}
		result = append(result, temp)
	}
	return
}
