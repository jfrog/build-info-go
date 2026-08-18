package flexpack

import "testing"

// TestScopesEqual_CountsOccurrences covers the case a membership check gets wrong: two
// slices of the same length drawn from the same distinct values, in different quantities.
func TestScopesEqual_CountsOccurrences(t *testing.T) {
	if scopesEqual([]string{"hello", "hello", "hello", "hey"}, []string{"hey", "hey", "hey", "hello"}) {
		t.Error("slices with the same distinct values but different counts must not be equal")
	}
	// Order must stay irrelevant, because scopes are merged through a map.
	if !scopesEqual([]string{"development", "test"}, []string{"test", "development"}) {
		t.Error("scope comparison must ignore order")
	}
	if !scopesEqual(nil, nil) {
		t.Error("two empty scope sets are equal")
	}
	if scopesEqual([]string{"default"}, nil) {
		t.Error("differing lengths must not be equal")
	}
	if scopesEqual([]string{"default"}, []string{"development"}) {
		t.Error("different scopes must not be equal")
	}
}
