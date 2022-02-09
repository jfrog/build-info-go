package utils

// StringSet is a set of unique strings.
type StringSet struct {
	m map[string]struct{}
}

func NewStringSet(strings ...string) *StringSet {
	res := &StringSet{
		m: map[string]struct{}{},
	}
	for _, s := range strings {
		res.Add(s)
	}
	return res
}

// Add adds a string to the set. If string is already in the set, it has no effect.
func (s *StringSet) Add(str string) {
	s.m[str] = struct{}{}
}

func (s *StringSet) AddAll(strings ...string) {
	for _, str := range strings {
		s.Add(str)
	}
}

// Delete removes a string from the set.
func (s *StringSet) Delete(str string) {
	delete(s.m, str)
}

func (s *StringSet) IsEmpty() bool {
	return len(s.m) == 0
}

func (s *StringSet) TotalStrings() int {
	return len(s.m)
}

// Strings returns strings in the set.
func (s *StringSet) ToSlice() []string {
	totalStringSet := len(s.m)
	if s.IsEmpty() {
		return nil
	}
	res := make([]string, 0, totalStringSet)
	for str := range s.m {
		res = append(res, str)
	}
	return res
}