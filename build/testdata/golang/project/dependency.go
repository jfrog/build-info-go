package dependency

import (
	"fmt"
	"github.com/Microsoft/go-winio"
	"github.com/jfrog/gofrog/version"
	"github.com/pkg/errors"
	"rsc.io/quote"
)

func PrintHello(ver *version.Version, re *winio.ReparsePoint) error {
	fmt.Println(quote.Hello())
	return errors.New("abc")
}
