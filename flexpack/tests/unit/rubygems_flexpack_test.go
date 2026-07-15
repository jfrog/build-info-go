package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
)

// sampleGemfile returns a minimal Gemfile.
func sampleGemfile() string {
	return `source "https://rubygems.org"

gem "rake"
gem "rspec"
`
}

// sampleGemfileLock returns a Gemfile.lock with a nested dependency tree:
//
//	rspec -> rspec-core -> rspec-support
//	rspec -> rspec-expectations -> diff-lcs, rspec-support
//	rake (direct, no deps)
func sampleGemfileLock() string {
	return `GEM
  remote: https://rubygems.org/
  specs:
    diff-lcs (1.5.0)
    rake (13.0.6)
    rspec (3.12.0)
      rspec-core (~> 3.12.0)
      rspec-expectations (~> 3.12.0)
    rspec-core (3.12.2)
      rspec-support (~> 3.12.0)
    rspec-expectations (3.12.3)
      diff-lcs (>= 1.2.0, < 2.0)
      rspec-support (~> 3.12.0)
    rspec-support (3.12.1)

PLATFORMS
  ruby

DEPENDENCIES
  rake
  rspec

BUNDLED WITH
   2.4.10
`
}

func writeGemTempFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", name, err)
		}
	}
}

func collectGemDeps(t *testing.T, dir string, cfg flexpack.GemConfig) (*entities.BuildInfo, *flexpack.RubygemsFlexPack) {
	t.Helper()
	cfg.WorkingDirectory = dir
	rf, err := flexpack.NewRubygemsFlexPack(cfg)
	if err != nil {
		t.Fatalf("NewRubygemsFlexPack failed: %v", err)
	}
	bi, err := rf.CollectBuildInfo("my-build", "1")
	if err != nil {
		t.Fatalf("CollectBuildInfo failed: %v", err)
	}
	return bi, rf
}

func TestNewRubygemsFlexPack(t *testing.T) {
	tempDir := t.TempDir()
	writeGemTempFiles(t, tempDir, map[string]string{
		"Gemfile":      sampleGemfile(),
		"Gemfile.lock": sampleGemfileLock(),
	})

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{})

	if len(bi.Modules) == 0 {
		t.Fatal("Expected at least one module")
	}
	if bi.Modules[0].Type != entities.Gem {
		t.Errorf("Expected module type %q, got %q", entities.Gem, bi.Modules[0].Type)
	}
	// Module ID falls back to working-directory base name (Gemfile has no name/version).
	if bi.Modules[0].Id != filepath.Base(tempDir) {
		t.Errorf("Expected module ID %q, got %q", filepath.Base(tempDir), bi.Modules[0].Id)
	}
}

func TestRubygemsAllDependenciesCollected(t *testing.T) {
	tempDir := t.TempDir()
	writeGemTempFiles(t, tempDir, map[string]string{
		"Gemfile":      sampleGemfile(),
		"Gemfile.lock": sampleGemfileLock(),
	})

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{})

	deps := bi.Modules[0].Dependencies
	want := map[string]string{
		"diff-lcs:1.5.0":            "gem",
		"rake:13.0.6":               "gem",
		"rspec:3.12.0":              "gem",
		"rspec-core:3.12.2":         "gem",
		"rspec-expectations:3.12.3": "gem",
		"rspec-support:3.12.1":      "gem",
	}
	if len(deps) != len(want) {
		t.Fatalf("Expected %d dependencies, got %d: %+v", len(want), len(deps), deps)
	}
	for _, dep := range deps {
		wantType, ok := want[dep.Id]
		if !ok {
			t.Errorf("Unexpected dependency ID %q", dep.Id)
			continue
		}
		if dep.Type != wantType {
			t.Errorf("Dependency %q: expected type %q, got %q", dep.Id, wantType, dep.Type)
		}
	}
}

func TestRubygemsRequestedByChains(t *testing.T) {
	tempDir := t.TempDir()
	writeGemTempFiles(t, tempDir, map[string]string{
		"Gemfile":      sampleGemfile(),
		"Gemfile.lock": sampleGemfileLock(),
	})

	_, rf := collectGemDeps(t, tempDir, flexpack.GemConfig{})
	chains := rf.GetRequestedByChains()

	// rspec-support is a transitive dep; it must be reachable from rspec via rspec-core
	// and rspec-expectations, terminating at the module root.
	supportChains := chains["rspec-support:3.12.1"]
	if len(supportChains) == 0 {
		t.Fatal("Expected requestedBy chains for rspec-support")
	}
	moduleID := filepath.Base(tempDir)
	foundRoot := false
	for _, chain := range supportChains {
		if len(chain) > 0 && chain[len(chain)-1] == moduleID {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Errorf("Expected at least one rspec-support chain to terminate at module root %q, got %+v", moduleID, supportChains)
	}

	// rake is a direct dependency: its chain is just the module root.
	rakeChains := chains["rake:13.0.6"]
	if len(rakeChains) != 1 || len(rakeChains[0]) != 1 || rakeChains[0][0] != moduleID {
		t.Errorf("Expected rake to be a direct dependency of %q, got %+v", moduleID, rakeChains)
	}
}

func TestRubygemsInstalledPackagesFilter(t *testing.T) {
	tempDir := t.TempDir()
	writeGemTempFiles(t, tempDir, map[string]string{
		"Gemfile":      sampleGemfile(),
		"Gemfile.lock": sampleGemfileLock(),
	})

	// Simulate `bundle install --without test` leaving only rake installed.
	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{
		InstalledPackages: map[string]string{"rake": "13.0.6"},
	})

	deps := bi.Modules[0].Dependencies
	if len(deps) != 1 {
		t.Fatalf("Expected only 1 installed dependency, got %d: %+v", len(deps), deps)
	}
	if deps[0].Id != "rake:13.0.6" {
		t.Errorf("Expected rake:13.0.6, got %q", deps[0].Id)
	}
}

func TestRubygemsProjectNameOverride(t *testing.T) {
	tempDir := t.TempDir()
	writeGemTempFiles(t, tempDir, map[string]string{
		"Gemfile.lock": sampleGemfileLock(),
	})

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{
		ProjectName:    "my-gem",
		ProjectVersion: "2.1.0",
	})
	if bi.Modules[0].Id != "my-gem:2.1.0" {
		t.Errorf("Expected module ID 'my-gem:2.1.0', got %q", bi.Modules[0].Id)
	}
}

func TestRubygemsGitPathDepsFlaggedDirectURL(t *testing.T) {
	tempDir := t.TempDir()
	lock := `GIT
  remote: https://github.com/example/my_gem.git
  revision: abc123
  specs:
    my_gem (0.1.0)

GEM
  remote: https://rubygems.org/
  specs:
    rake (13.0.6)

PLATFORMS
  ruby

DEPENDENCIES
  my_gem!
  rake
`
	writeGemTempFiles(t, tempDir, map[string]string{"Gemfile.lock": lock})

	_, rf := collectGemDeps(t, tempDir, flexpack.GemConfig{})
	directURLs := rf.GetDirectURLDeps()
	if _, ok := directURLs["my_gem:0.1.0"]; !ok {
		t.Errorf("Expected my_gem:0.1.0 to be flagged as a direct-URL (GIT) dependency, got %+v", directURLs)
	}
	if _, ok := directURLs["rake:13.0.6"]; ok {
		t.Error("rake is a registry gem and must not be flagged as direct-URL")
	}
}

func TestRubygemsEmptyLockNoError(t *testing.T) {
	tempDir := t.TempDir()
	// No Gemfile.lock present — must not error, just produce an empty module.
	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{})
	if len(bi.Modules) == 0 {
		t.Fatal("Expected a module even with no lock file")
	}
	if len(bi.Modules[0].Dependencies) != 0 {
		t.Errorf("Expected no dependencies, got %d", len(bi.Modules[0].Dependencies))
	}
}
