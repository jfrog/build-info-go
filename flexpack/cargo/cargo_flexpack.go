package cargo

import (
	"os/exec"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/log"
)

// CargoFlexPack implements FlexPack build-info collection for Cargo.
type CargoFlexPack struct {
	config        CargoConfig
	dependencies  []entities.Dependency
	meta          *CargoMetadata
	lockChecksums map[string]string // name|version -> sha256
	initialized   bool
}

func NewCargoFlexPack(config CargoConfig) (*CargoFlexPack, error) {
	cf := &CargoFlexPack{config: config, lockChecksums: map[string]string{}}
	if cf.config.CargoExecutable == "" {
		if p, err := exec.LookPath("cargo"); err == nil {
			cf.config.CargoExecutable = p
		} else {
			log.Warn("cargo executable not found in PATH, using 'cargo'")
			cf.config.CargoExecutable = "cargo"
		}
	}
	return cf, nil
}
