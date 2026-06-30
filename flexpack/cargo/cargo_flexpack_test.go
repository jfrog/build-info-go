package cargo

import (
	"os"
	"testing"

	"github.com/jfrog/build-info-go/entities"
)

func TestCollectBuildInfoProducesCargoModule(t *testing.T) {
	data, _ := os.ReadFile("testdata/metadata.json")
	meta, _ := parseMetadata(data)
	lock, _ := parseLockfile("testdata/Cargo.lock")
	cf := &CargoFlexPack{config: CargoConfig{}, meta: meta, lockChecksums: lock, initialized: true}
	t.Setenv("CARGO_HOME", t.TempDir())
	if err := cf.collectDependenciesFromMeta(); err != nil {
		t.Fatal(err)
	}
	bi, err := cf.buildInfoFromState("my-build", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bi.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(bi.Modules))
	}
	if bi.Modules[0].Type != entities.Cargo {
		t.Errorf("module type = %q, want cargo", bi.Modules[0].Type)
	}
	if bi.Modules[0].Id != "root:0.1.0" {
		t.Errorf("module id = %q, want root:0.1.0", bi.Modules[0].Id)
	}
}
