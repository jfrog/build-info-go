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
