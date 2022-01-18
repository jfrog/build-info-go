package dependency

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/jfrog/gofrog/version"
	"github.com/pkg/errors"
	"rsc.io/quote"
)

func PrintHello(ver *version.Version, re *toml.Key) error {
	fmt.Println(quote.Hello())
	return errors.New("abc")
}
