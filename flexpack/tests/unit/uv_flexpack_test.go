package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/flexpack"
)

// minimalPyproject returns a minimal valid PEP 621 pyproject.toml content.
func minimalPyproject(name string) string {
	return "[project]\nname = \"" + name + "\"\nversion = \"1.0.0\"\n"
}

// minimalUvLock returns a uv.lock with a virtual root and one registry package.
func minimalUvLock(projectName string) string {
	return `version = 1

[[package]]
name = "` + projectName + `"
version = "1.0.0"
source = { virtual = "." }

[[package]]
name = "requests"
version = "2.31.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/requests-2.31.0-py3-none-any.whl"
hash = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
size = 62574
`
}

func writeTempFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", name, err)
		}
	}
}

func TestNewUvFlexPack(t *testing.T) {
	tempDir := t.TempDir()
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("test-app"),
		"uv.lock":        minimalUvLock("test-app"),
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}

	buildInfo, err := uf.CollectBuildInfo("my-build", "1")
	if err != nil {
		t.Fatalf("CollectBuildInfo failed: %v", err)
	}

	if len(buildInfo.Modules) == 0 {
		t.Fatal("Expected at least one module")
	}
	if buildInfo.Modules[0].Id != "test-app:1.0.0" {
		t.Errorf("Expected module ID 'test-app:1.0.0', got '%s'", buildInfo.Modules[0].Id)
	}
}

func TestUvCollectBuildInfoModuleType(t *testing.T) {
	tempDir := t.TempDir()
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("test-app"),
		"uv.lock":        minimalUvLock("test-app"),
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}

	buildInfo, err := uf.CollectBuildInfo("my-build", "1")
	if err != nil {
		t.Fatalf("CollectBuildInfo failed: %v", err)
	}

	if len(buildInfo.Modules) == 0 {
		t.Fatal("Expected at least one module")
	}
	if buildInfo.Modules[0].Type != "uv" {
		t.Errorf("Expected module type 'uv', got '%s'", buildInfo.Modules[0].Type)
	}
}

func TestUvHashExtraction(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package]]
name = "requests"
version = "2.31.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/requests-2.31.0-py3-none-any.whl"
hash = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
size = 62574
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}

	deps, err := uf.GetProjectDependencies()
	if err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}

	if len(deps) == 0 {
		t.Fatal("Expected at least one dependency")
	}

	// SHA256 should be stripped of the "sha256:" prefix
	expectedSHA256 := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	if deps[0].SHA256 != expectedSHA256 {
		t.Errorf("Expected SHA256 '%s', got '%s'", expectedSHA256, deps[0].SHA256)
	}
}

func TestUvVirtualSourceSkipped(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package]]
name = "certifi"
version = "2023.7.22"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/certifi-2023.7.22-py3-none-any.whl"
hash = "sha256:aaaabbbbccccddddeeeeffffaaaabbbbccccddddeeeeffffaaaabbbbccccdddd"
size = 158
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}

	deps, err := uf.GetProjectDependencies()
	if err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}

	// my-app (virtual source) must not appear in deps
	for _, dep := range deps {
		if dep.Name == "my-app" {
			t.Errorf("Virtual source package 'my-app' should not appear in dependencies")
		}
	}
}

func TestUvGitDependency(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package]]
name = "some-lib"
version = "0.1.0"
source = { git = "https://github.com/example/some-lib?tag=v0.1.0#abcdef1234567890abcdef1234567890abcdef12" }
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}

	deps, err := uf.GetProjectDependencies()
	if err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}

	found := false
	for _, dep := range deps {
		if dep.Name == "some-lib" {
			found = true
			if dep.SHA256 != "" {
				t.Errorf("Expected empty SHA256 for git dependency, got '%s'", dep.SHA256)
			}
			break
		}
	}
	if !found {
		t.Error("Expected git dependency 'some-lib' to appear in dependencies")
	}
}

func TestUvDevDependencies(t *testing.T) {
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[package.dev-dependencies]
dev = [
    { name = "pytest" },
]

[[package]]
name = "requests"
version = "2.31.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/requests-2.31.0-py3-none-any.whl"
hash = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
size = 62574

[[package]]
name = "pytest"
version = "7.4.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/pytest-7.4.0-py3-none-any.whl"
hash = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
size = 324467
`

	t.Run("include dev deps", func(t *testing.T) {
		tempDir := t.TempDir()
		writeTempFiles(t, tempDir, map[string]string{
			"pyproject.toml": minimalPyproject("my-app"),
			"uv.lock":        uvLockContent,
		})

		uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{
			WorkingDirectory:       tempDir,
			IncludeDevDependencies: true,
		})
		if err != nil {
			t.Fatalf("NewUvFlexPack failed: %v", err)
		}

		deps, err := uf.GetProjectDependencies()
		if err != nil {
			t.Fatalf("GetProjectDependencies failed: %v", err)
		}

		found := false
		for _, dep := range deps {
			if dep.Name == "pytest" {
				found = true
				// No scopes — Python has no compile/runtime distinction (matches pip/pipenv)
				if len(dep.Scopes) != 0 {
					t.Errorf("Expected no scopes for dev dep (Python has no scope distinction), got %v", dep.Scopes)
				}
				break
			}
		}
		if !found {
			t.Error("Expected 'pytest' dev dependency to be included when IncludeDevDependencies=true")
		}
	})

	t.Run("exclude dev deps", func(t *testing.T) {
		tempDir := t.TempDir()
		writeTempFiles(t, tempDir, map[string]string{
			"pyproject.toml": minimalPyproject("my-app"),
			"uv.lock":        uvLockContent,
		})

		uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{
			WorkingDirectory:       tempDir,
			IncludeDevDependencies: false,
		})
		if err != nil {
			t.Fatalf("NewUvFlexPack failed: %v", err)
		}

		deps, err := uf.GetProjectDependencies()
		if err != nil {
			t.Fatalf("GetProjectDependencies failed: %v", err)
		}

		for _, dep := range deps {
			if dep.Name == "pytest" {
				t.Error("Expected 'pytest' dev dependency to be excluded when IncludeDevDependencies=false")
			}
		}
	})
}

// TestUvWheelSelectionNoneAny verifies that bestHash/depFileType prefer the
// pure-Python none-any wheel over platform-specific wheels, and fall back to
// the first available wheel when none-any is absent.
func TestUvWheelSelectionNoneAny(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package]]
name = "multiplatform-pkg"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/multiplatform_pkg-1.0.0-cp311-cp311-linux_x86_64.whl"
hash = "sha256:linux000000000000000000000000000000000000000000000000000000000000"
size = 1000

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/multiplatform_pkg-1.0.0-py3-none-any.whl"
hash = "sha256:noneany00000000000000000000000000000000000000000000000000000000"
size = 900

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/multiplatform_pkg-1.0.0-cp311-cp311-win_amd64.whl"
hash = "sha256:windows0000000000000000000000000000000000000000000000000000000000"
size = 1100
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}
	deps, err := uf.GetProjectDependencies()
	if err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}
	if len(deps) == 0 {
		t.Fatal("Expected at least one dependency")
	}
	dep := deps[0]
	// ID is now name:version (pip format); sha256 still uses none-any wheel hash
	if dep.ID != "multiplatform-pkg:1.0.0" {
		t.Errorf("Expected name:version ID, got %q", dep.ID)
	}
	if dep.Type != "whl" {
		t.Errorf("Expected type 'whl', got %q", dep.Type)
	}
	if dep.SHA256 != "noneany00000000000000000000000000000000000000000000000000000000" {
		t.Errorf("Expected none-any sha256, got %q", dep.SHA256)
	}
}

// TestUvWheelSelectionFirstFallback verifies that when there is no none-any wheel,
// the first wheel in the list is used as a fallback.
func TestUvWheelSelectionFirstFallback(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package]]
name = "platform-only-pkg"
version = "2.0.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/platform_only_pkg-2.0.0-cp311-cp311-linux_x86_64.whl"
hash = "sha256:firstwheel000000000000000000000000000000000000000000000000000000"
size = 2000

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/platform_only_pkg-2.0.0-cp311-cp311-win_amd64.whl"
hash = "sha256:secondwheel00000000000000000000000000000000000000000000000000000"
size = 2100
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}
	deps, err := uf.GetProjectDependencies()
	if err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}
	if len(deps) == 0 {
		t.Fatal("Expected at least one dependency")
	}
	dep := deps[0]
	// ID is name:version; type is the file extension of the selected wheel
	if dep.ID != "platform-only-pkg:2.0.0" {
		t.Errorf("Expected name:version ID, got %q", dep.ID)
	}
	if dep.Type != "whl" {
		t.Errorf("Expected type 'whl', got %q", dep.Type)
	}
	if dep.SHA256 != "firstwheel000000000000000000000000000000000000000000000000000000" {
		t.Errorf("Expected first wheel sha256, got %q", dep.SHA256)
	}
}

// TestUvWheelSelectionSdistFallback verifies that when no wheels are present,
// the sdist entry is used as a fallback.
func TestUvWheelSelectionSdistFallback(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package]]
name = "sdist-only-pkg"
version = "3.0.0"
source = { registry = "https://pypi.org/simple" }

[package.sdist]
url = "https://files.pythonhosted.org/packages/sdist_only_pkg-3.0.0.tar.gz"
hash = "sha256:sdist000000000000000000000000000000000000000000000000000000000000"
size = 5000
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}
	deps, err := uf.GetProjectDependencies()
	if err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}
	if len(deps) == 0 {
		t.Fatal("Expected at least one dependency")
	}
	dep := deps[0]
	// ID is name:version; type is tar.gz for sdist
	if dep.ID != "sdist-only-pkg:3.0.0" {
		t.Errorf("Expected name:version ID, got %q", dep.ID)
	}
	if dep.Type != "tar.gz" {
		t.Errorf("Expected type 'tar.gz', got %q", dep.Type)
	}
	if dep.SHA256 != "sdist000000000000000000000000000000000000000000000000000000000000" {
		t.Errorf("Expected sdist sha256, got %q", dep.SHA256)
	}
}

// TestUvRequestedByChain verifies that the requestedBy inverse mapping is
// constructed correctly from a transitive dependency chain: root→A→B→C.
// C.requestedBy should contain B, B.requestedBy should contain A.
func TestUvRequestedByChain(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package.dependencies]]
name = "pkga"

[[package]]
name = "pkga"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }

[[package.dependencies]]
name = "pkgb"

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/pkga-1.0.0-py3-none-any.whl"
hash = "sha256:aaaa000000000000000000000000000000000000000000000000000000000000"
size = 100

[[package]]
name = "pkgb"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }

[[package.dependencies]]
name = "pkgc"

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/pkgb-1.0.0-py3-none-any.whl"
hash = "sha256:bbbb000000000000000000000000000000000000000000000000000000000000"
size = 100

[[package]]
name = "pkgc"
version = "1.0.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/pkgc-1.0.0-py3-none-any.whl"
hash = "sha256:cccc000000000000000000000000000000000000000000000000000000000000"
size = 100
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}
	if _, err := uf.GetProjectDependencies(); err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}

	// Use GetRequestedByChains() to inspect full [][]string chains (UV-specific).
	// DependencyInfo.RequestedBy is []string (shared type); chains are stored separately.
	chains := uf.GetRequestedByChains()

	// pkgb must report pkga as its direct requester (chain: [pkga:1.0.0, my-app:1.0.0])
	pkgbChains, ok := chains["pkgb:1.0.0"]
	if !ok || len(pkgbChains) == 0 {
		t.Error("pkgb:1.0.0 should have at least one requestedBy chain (requested by pkga)")
	} else {
		found := false
		for _, chain := range pkgbChains {
			if len(chain) > 0 && chain[0] == "pkga:1.0.0" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pkgb:1.0.0 requestedBy chains should start with 'pkga:1.0.0', got %v", pkgbChains)
		}
	}

	// pkgc must report pkgb as its direct requester (chain: [pkgb:1.0.0, pkga:1.0.0, my-app:1.0.0])
	pkgcChains, ok2 := chains["pkgc:1.0.0"]
	if !ok2 || len(pkgcChains) == 0 {
		t.Error("pkgc:1.0.0 should have at least one requestedBy chain (requested by pkgb)")
	} else {
		found := false
		for _, chain := range pkgcChains {
			if len(chain) > 0 && chain[0] == "pkgb:1.0.0" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pkgc:1.0.0 requestedBy chains should start with 'pkgb:1.0.0', got %v", pkgcChains)
		}
	}
}

// TestUvMultipleDependencies verifies that all direct dependencies declared in
// pyproject.toml appear in the build info with correct count.
func TestUvMultipleDependencies(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package]]
name = "certifi"
version = "2024.2.2"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/certifi-2024.2.2-py3-none-any.whl"
hash = "sha256:cert00000000000000000000000000000000000000000000000000000000000000"
size = 163674

[[package]]
name = "requests"
version = "2.31.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/requests-2.31.0-py3-none-any.whl"
hash = "sha256:reqs00000000000000000000000000000000000000000000000000000000000000"
size = 62574

[[package]]
name = "urllib3"
version = "2.0.7"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/urllib3-2.0.7-py3-none-any.whl"
hash = "sha256:url000000000000000000000000000000000000000000000000000000000000000"
size = 124424
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{WorkingDirectory: tempDir})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}
	deps, err := uf.GetProjectDependencies()
	if err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}

	if len(deps) != 3 {
		t.Errorf("Expected 3 dependencies (certifi, requests, urllib3), got %d", len(deps))
	}
	names := make(map[string]bool)
	for _, d := range deps {
		names[d.Name] = true
	}
	for _, expected := range []string{"certifi", "requests", "urllib3"} {
		if !names[expected] {
			t.Errorf("Expected dependency %q not found in: %v", expected, deps)
		}
	}
}

// TestUvDevDepInclusion verifies that dev deps are excluded by default and
// included when IncludeDevDependencies=true. No scopes are set (Python has no
// compile/runtime distinction — matches pip/pipenv canonical format).
func TestUvScopeClassification(t *testing.T) {
	tempDir := t.TempDir()
	uvLockContent := `version = 1

[[package]]
name = "my-app"
version = "1.0.0"
source = { virtual = "." }

[[package.dependencies]]
name = "certifi"

[package.dev-dependencies]
dev = [
    { name = "pytest" },
]

[[package]]
name = "certifi"
version = "2024.2.2"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/certifi-2024.2.2-py3-none-any.whl"
hash = "sha256:cert00000000000000000000000000000000000000000000000000000000000000"
size = 163674

[[package]]
name = "pytest"
version = "7.4.0"
source = { registry = "https://pypi.org/simple" }

[[package.wheels]]
url = "https://files.pythonhosted.org/packages/pytest-7.4.0-py3-none-any.whl"
hash = "sha256:pytest00000000000000000000000000000000000000000000000000000000000"
size = 324467
`
	writeTempFiles(t, tempDir, map[string]string{
		"pyproject.toml": minimalPyproject("my-app"),
		"uv.lock":        uvLockContent,
	})

	uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{
		WorkingDirectory:       tempDir,
		IncludeDevDependencies: true,
	})
	if err != nil {
		t.Fatalf("NewUvFlexPack failed: %v", err)
	}
	deps, err := uf.GetProjectDependencies()
	if err != nil {
		t.Fatalf("GetProjectDependencies failed: %v", err)
	}

	depByName := make(map[string]flexpack.DependencyInfo)
	for _, d := range deps {
		depByName[d.Name] = d
	}

	certifi, ok := depByName["certifi"]
	if !ok {
		t.Fatal("Expected certifi in dependencies")
	}
	// No scopes — Python has no compile/runtime distinction (matches pip/pipenv)
	if len(certifi.Scopes) != 0 {
		t.Errorf("certifi should have no scopes (Python has no compile/runtime distinction), got %v", certifi.Scopes)
	}
	if certifi.Type != "whl" {
		t.Errorf("certifi type should be 'whl', got %q", certifi.Type)
	}

	pytest, ok := depByName["pytest"]
	if !ok {
		t.Fatal("Expected pytest in dependencies (IncludeDevDependencies=true)")
	}
	// Dev deps also have no scopes in the new format
	if len(pytest.Scopes) != 0 {
		t.Errorf("pytest should have no scopes, got %v", pytest.Scopes)
	}
}

func TestUvErrorHandling(t *testing.T) {
	t.Run("non-existent dir", func(t *testing.T) {
		_, err := flexpack.NewUvFlexPack(flexpack.UvConfig{
			WorkingDirectory: "/non/existent/directory",
		})
		if err == nil {
			t.Error("Expected error for non-existent directory")
		}
	})

	t.Run("dir with no pyproject.toml", func(t *testing.T) {
		tempDir := t.TempDir()
		_, err := flexpack.NewUvFlexPack(flexpack.UvConfig{
			WorkingDirectory: tempDir,
		})
		if err == nil {
			t.Error("Expected error when pyproject.toml is missing")
		}
	})

	t.Run("pyproject.toml present but no uv.lock", func(t *testing.T) {
		tempDir := t.TempDir()
		writeTempFiles(t, tempDir, map[string]string{
			"pyproject.toml": minimalPyproject("my-app"),
		})

		uf, err := flexpack.NewUvFlexPack(flexpack.UvConfig{
			WorkingDirectory: tempDir,
		})
		if err != nil {
			t.Fatalf("Expected constructor to succeed without uv.lock, got: %v", err)
		}

		deps, err := uf.GetProjectDependencies()
		if err != nil {
			t.Fatalf("GetProjectDependencies failed: %v", err)
		}
		if len(deps) != 0 {
			t.Errorf("Expected empty dependency list without uv.lock, got %d deps", len(deps))
		}
	})
}
