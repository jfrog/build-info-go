package utils

import (
	"regexp"
)

func GetRegExp(regex string) (*regexp.Regexp, error) {
	regExp, err := regexp.Compile(regex)
	if err != nil {
		return nil, err
	}
	return regExp, nil
}

// StripSurroundingQuotes removes a matching pair of leading/trailing single or double quotes from value,
// e.g., 'val ue' => val ue. If value is too short, or its leading and trailing characters aren't a matching
// quote pair, it is returned unchanged.
func StripSurroundingQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	first, last := value[0], value[len(value)-1]
	if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
		return value[1 : len(value)-1]
	}
	return value
}
