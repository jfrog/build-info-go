package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/build-info-go/flexpack"
)

// writeFakeBundleExecutable writes an executable script that ignores its arguments and
// prints jsonOutput to stdout, standing in for `bundle exec ruby -e ...` in tests so they
// don't need a real Ruby/Bundler environment. This mirrors how other FlexPack tests in
// this repo (e.g. Conan's) override their *Executable config field with a stand-in binary.
func writeFakeBundleExecutable(t *testing.T, dir string, jsonOutput string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake bundle executable script requires a POSIX shell")
	}
	scriptPath := filepath.Join(dir, "fake-bundle.sh")
	content := "#!/bin/sh\ncat <<'BUNDLE_EOF'\n" + jsonOutput + "\nBUNDLE_EOF\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		t.Fatalf("Failed to write fake bundle executable: %v", err)
	}
	return scriptPath
}

// writeFailingBundleExecutable writes an executable script that always exits non-zero,
// simulating `bundle exec` failing (e.g. no Gemfile, bundler not installed).
func writeFailingBundleExecutable(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake bundle executable script requires a POSIX shell")
	}
	scriptPath := filepath.Join(dir, "fake-bundle-fail.sh")
	content := "#!/bin/sh\necho 'Could not locate Gemfile' >&2\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		t.Fatalf("Failed to write failing bundle executable: %v", err)
	}
	return scriptPath
}

// sampleBundlerGraphJSON returns the JSON a `bundle exec ruby` dependency-graph query
// would report for a nested dependency tree:
//
//	rspec (direct, group "default") -> rspec-core -> rspec-support
//	rspec (direct, group "default") -> rspec-expectations -> diff-lcs, rspec-support
//	rake (direct, group "default", no deps)
//
// Also includes Bundler's own "bundler" entry, present in every real Bundler.definition
// but never a real project dependency.
func sampleBundlerGraphJSON() string {
	return `[
		{"name":"bundler","version":"2.4.10","deps":[],"groups":null,"source":"GEM","remote":null},
		{"name":"diff-lcs","version":"1.5.0","deps":[],"groups":null,"source":"GEM","remote":null},
		{"name":"rake","version":"13.0.6","deps":[],"groups":["default"],"source":"GEM","remote":null},
		{"name":"rspec","version":"3.12.0","deps":["rspec-core","rspec-expectations"],"groups":["default"],"source":"GEM","remote":null},
		{"name":"rspec-core","version":"3.12.2","deps":["rspec-support"],"groups":null,"source":"GEM","remote":null},
		{"name":"rspec-expectations","version":"3.12.3","deps":["diff-lcs","rspec-support"],"groups":null,"source":"GEM","remote":null},
		{"name":"rspec-support","version":"3.12.1","deps":[],"groups":null,"source":"GEM","remote":null}
	]`
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
	bundleExe := writeFakeBundleExecutable(t, tempDir, sampleBundlerGraphJSON())

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{BundleExecutable: bundleExe})

	if len(bi.Modules) == 0 {
		t.Fatal("Expected at least one module")
	}
	if bi.Modules[0].Type != entities.Gem {
		t.Errorf("Expected module type %q, got %q", entities.Gem, bi.Modules[0].Type)
	}
	// Module ID falls back to working-directory base name (no ProjectName override).
	if bi.Modules[0].Id != filepath.Base(tempDir) {
		t.Errorf("Expected module ID %q, got %q", filepath.Base(tempDir), bi.Modules[0].Id)
	}
}

func TestRubygemsAllDependenciesCollected(t *testing.T) {
	tempDir := t.TempDir()
	bundleExe := writeFakeBundleExecutable(t, tempDir, sampleBundlerGraphJSON())

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{BundleExecutable: bundleExe})

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

// TestRubygemsExcludesBundlerSelfGem verifies that Bundler's own "bundler" entry —
// always present in Bundler.definition.specs — is never surfaced as a project dependency.
func TestRubygemsExcludesBundlerSelfGem(t *testing.T) {
	tempDir := t.TempDir()
	bundleExe := writeFakeBundleExecutable(t, tempDir, sampleBundlerGraphJSON())

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{BundleExecutable: bundleExe})

	for _, dep := range bi.Modules[0].Dependencies {
		if dep.Id == "bundler:2.4.10" {
			t.Errorf("bundler's own gem must not be reported as a project dependency, got %+v", dep)
		}
	}
}

// TestRubygemsScopesUseBundlerGroupNames verifies that scopes come from Bundler's own
// group names ("default"), never a hardcoded "production" label Bundler doesn't use.
func TestRubygemsScopesUseBundlerGroupNames(t *testing.T) {
	tempDir := t.TempDir()
	bundleExe := writeFakeBundleExecutable(t, tempDir, sampleBundlerGraphJSON())

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{BundleExecutable: bundleExe})

	for _, dep := range bi.Modules[0].Dependencies {
		for _, scope := range dep.Scopes {
			if scope == "production" {
				t.Errorf("Dependency %q must not carry the hardcoded \"production\" scope, got %+v", dep.Id, dep.Scopes)
			}
		}
		if dep.Id == "rake:13.0.6" {
			if len(dep.Scopes) != 1 || dep.Scopes[0] != "default" {
				t.Errorf("Expected rake to carry Bundler's own \"default\" group, got %+v", dep.Scopes)
			}
		}
	}
}

func TestRubygemsRequestedByChains(t *testing.T) {
	tempDir := t.TempDir()
	bundleExe := writeFakeBundleExecutable(t, tempDir, sampleBundlerGraphJSON())

	_, rf := collectGemDeps(t, tempDir, flexpack.GemConfig{BundleExecutable: bundleExe})
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
	bundleExe := writeFakeBundleExecutable(t, tempDir, sampleBundlerGraphJSON())

	// Simulate `bundle install --without test` leaving only rake installed.
	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{
		BundleExecutable:  bundleExe,
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
	bundleExe := writeFakeBundleExecutable(t, tempDir, sampleBundlerGraphJSON())

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{
		BundleExecutable: bundleExe,
		ProjectName:      "my-gem",
		ProjectVersion:   "2.1.0",
	})
	if bi.Modules[0].Id != "my-gem:2.1.0" {
		t.Errorf("Expected module ID 'my-gem:2.1.0', got %q", bi.Modules[0].Id)
	}
}

func TestRubygemsGitPathDepsFlaggedDirectURL(t *testing.T) {
	tempDir := t.TempDir()
	graphJSON := `[
		{"name":"my_gem","version":"0.1.0","deps":[],"groups":["default"],"source":"GIT","remote":"https://github.com/example/my_gem.git"},
		{"name":"rake","version":"13.0.6","deps":[],"groups":["default"],"source":"GEM","remote":null}
	]`
	bundleExe := writeFakeBundleExecutable(t, tempDir, graphJSON)

	_, rf := collectGemDeps(t, tempDir, flexpack.GemConfig{BundleExecutable: bundleExe})
	directURLs := rf.GetDirectURLDeps()
	if _, ok := directURLs["my_gem:0.1.0"]; !ok {
		t.Errorf("Expected my_gem:0.1.0 to be flagged as a direct-URL (GIT) dependency, got %+v", directURLs)
	}
	if _, ok := directURLs["rake:13.0.6"]; ok {
		t.Error("rake is a registry gem and must not be flagged as direct-URL")
	}
}

func TestRubygemsPathDepFlaggedDirectURL(t *testing.T) {
	tempDir := t.TempDir()
	graphJSON := `[{"name":"my_gem","version":"0.1.0","deps":[],"groups":["default"],"source":"PATH","remote":"vendor/my_gem"}]`
	bundleExe := writeFakeBundleExecutable(t, tempDir, graphJSON)

	_, rf := collectGemDeps(t, tempDir, flexpack.GemConfig{BundleExecutable: bundleExe})
	directURLs := rf.GetDirectURLDeps()
	if url, ok := directURLs["my_gem:0.1.0"]; !ok || url != "vendor/my_gem" {
		t.Errorf("Expected my_gem:0.1.0 to be flagged as a direct-URL (PATH) dependency with its path, got %+v", directURLs)
	}
}

func TestRubygemsEmptyLockNoError(t *testing.T) {
	tempDir := t.TempDir()
	// Simulate no Gemfile present / bundler unavailable — must not error, just produce
	// an empty module.
	bundleExe := writeFailingBundleExecutable(t, tempDir)

	bi, _ := collectGemDeps(t, tempDir, flexpack.GemConfig{BundleExecutable: bundleExe})
	if len(bi.Modules) == 0 {
		t.Fatal("Expected a module even when the Bundler query fails")
	}
	if len(bi.Modules[0].Dependencies) != 0 {
		t.Errorf("Expected no dependencies, got %d", len(bi.Modules[0].Dependencies))
	}
}
