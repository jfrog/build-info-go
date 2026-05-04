package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfrog/build-info-go/entities"
	nixflex "github.com/jfrog/build-info-go/flexpack/nix"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const realWorldFlakeLock = `{
  "nodes": {
    "flake-utils": {
      "inputs": { "systems": "systems" },
      "locked": {
        "lastModified": 1710146030,
        "narHash": "sha256-SZ5L6eA7HJ/nmkzGG7/ISclqe6oZdOZTNoesiInkXPQ=",
        "owner": "numtide",
        "repo": "flake-utils",
        "rev": "b1d9ab70662946ef0850d488da1c9019f3a9752a",
        "type": "github"
      },
      "original": { "owner": "numtide", "repo": "flake-utils", "type": "github" }
    },
    "nixpkgs": {
      "locked": {
        "lastModified": 1710272261,
        "narHash": "sha256-g+z7DFEIGGxPcQ4kDsSlFNzXJVhqPiGMrx0cPYrGnNA=",
        "owner": "NixOS",
        "repo": "nixpkgs",
        "rev": "0ad13a6833440b8e238947e47bea7f11071dc2b2",
        "type": "github"
      },
      "original": { "owner": "NixOS", "ref": "nixos-unstable", "repo": "nixpkgs", "type": "github" }
    },
    "root": {
      "inputs": { "flake-utils": "flake-utils", "nixpkgs": "nixpkgs" }
    },
    "systems": {
      "locked": {
        "lastModified": 1681028828,
        "narHash": "sha256-Vy1rq5AaRuLzOxct8nz4T6wlgyUR7zLU309k9mBC768=",
        "owner": "nix-systems",
        "repo": "default",
        "rev": "da67096a3b9bf56a91d16901293e51ba5b49a27e",
        "type": "github"
      },
      "original": { "owner": "nix-systems", "repo": "default", "type": "github" }
    }
  },
  "root": "root",
  "version": 7
}`

func TestNewNixFlexPack(t *testing.T) {
	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, realWorldFlakeLock)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, nf)

	buildInfo, err := nf.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)
	assert.NotNil(t, buildInfo)
	require.Len(t, buildInfo.Modules, 1)
	assert.Equal(t, entities.Nix, buildInfo.Modules[0].Type)
	assert.Equal(t, 3, len(buildInfo.Modules[0].Dependencies))
}

func TestNixFlexPackMissingFlakeNix(t *testing.T) {
	tempDir := t.TempDir()
	writeFlakeLock(t, tempDir, realWorldFlakeLock)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	_, err := nixflex.NewNixFlexPack(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "flake.nix not found")
}

func TestNixFlexPackMissingFlakeLock(t *testing.T) {
	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)
	assert.NotNil(t, nf)

	deps, err := nf.GetProjectDependencies()
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestNixFlexPackFollowsAliases(t *testing.T) {
	lockContent := `{
  "nodes": {
    "nix": {
      "inputs": { "nixpkgs": "nixpkgs_2" },
      "locked": {
        "lastModified": 1710000000,
        "narHash": "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
        "owner": "NixOS",
        "repo": "nix",
        "rev": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "type": "github"
      },
      "original": { "owner": "NixOS", "repo": "nix", "type": "github" }
    },
    "nixpkgs": {
      "locked": {
        "lastModified": 1710272261,
        "narHash": "sha256-g+z7DFEIGGxPcQ4kDsSlFNzXJVhqPiGMrx0cPYrGnNA=",
        "owner": "NixOS",
        "repo": "nixpkgs",
        "rev": "0ad13a6833440b8e238947e47bea7f11071dc2b2",
        "type": "github"
      },
      "original": { "owner": "NixOS", "ref": "nixos-unstable", "repo": "nixpkgs", "type": "github" }
    },
    "nixpkgs_2": {
      "locked": {
        "lastModified": 1709000000,
        "narHash": "sha256-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
        "owner": "NixOS",
        "repo": "nixpkgs",
        "rev": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        "type": "github"
      },
      "original": { "owner": "NixOS", "repo": "nixpkgs", "type": "github" }
    },
    "root": {
      "inputs": {
        "nix": "nix",
        "nixpkgs": "nixpkgs"
      }
    }
  },
  "root": "root",
  "version": 7
}`

	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, lockContent)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)

	deps, err := nf.GetProjectDependencies()
	require.NoError(t, err)
	// Should have 3 real deps: nix, nixpkgs, nixpkgs_2 — no alias nodes
	assert.Equal(t, 3, len(deps))

	// Verify no duplicates
	seen := make(map[string]bool)
	for _, dep := range deps {
		assert.False(t, seen[dep.ID], "duplicate dependency: %s", dep.ID)
		seen[dep.ID] = true
	}
}

func TestNixFlexPackDependencyGraph(t *testing.T) {
	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, realWorldFlakeLock)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)

	graph, err := nf.GetDependencyGraph()
	require.NoError(t, err)
	assert.NotEmpty(t, graph)

	// Root project should have edges to its direct inputs
	projectName := filepath.Base(tempDir)
	rootChildren, exists := graph[projectName]
	assert.True(t, exists, "root project should be in graph")
	assert.GreaterOrEqual(t, len(rootChildren), 2, "root should have at least 2 direct inputs")

	// Verify RequestedBy inversion
	requestedBy := nf.CalculateRequestedBy()
	assert.NotEmpty(t, requestedBy)

	// Systems should be requested by flake-utils
	for depID, parents := range requestedBy {
		if depID == "systems:da67096a3b9bf56a91d16901293e51ba5b49a27e" {
			assert.Contains(t, parents, "flake-utils:b1d9ab70662946ef0850d488da1c9019f3a9752a")
		}
	}
}

func TestNixFlexPackChecksums(t *testing.T) {
	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, realWorldFlakeLock)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)

	checksums := nf.CalculateChecksum()
	assert.Equal(t, 3, len(checksums))

	for _, cs := range checksums {
		sha256, ok := cs["sha256"].(string)
		assert.True(t, ok, "sha256 should be a string")
		assert.True(t, len(sha256) > 0, "sha256 should not be empty")
		// Verify SRI format: sha256-<base64>=
		assert.True(t, len(sha256) > 7 && sha256[:7] == "sha256-", "sha256 should be in SRI format, got: %s", sha256)
	}

	// Also verify via CollectBuildInfo
	buildInfo, err := nf.CollectBuildInfo("test-build", "1")
	require.NoError(t, err)
	for _, dep := range buildInfo.Modules[0].Dependencies {
		assert.NotEmpty(t, dep.Checksum.Sha256)
		assert.True(t, len(dep.Checksum.Sha256) > 7 && dep.Checksum.Sha256[:7] == "sha256-",
			"Sha256 should be SRI format, got: %s", dep.Checksum.Sha256)
	}
}

func TestNixFlexPackRealWorldLock(t *testing.T) {
	// Test with the realistic fixture including github inputs, flake: false nodes
	lockContent := `{
  "nodes": {
    "flake-compat": {
      "flake": false,
      "locked": {
        "lastModified": 1696426674,
        "narHash": "sha256-kvjfFW7WAETZlt09AgDn1MrtKzP7t90Vf7isPyY01aI=",
        "owner": "edolstra",
        "repo": "flake-compat",
        "rev": "0f9255e01c2351aa7d5b4868298bb91f927e393e",
        "type": "github"
      },
      "original": { "owner": "edolstra", "repo": "flake-compat", "type": "github" }
    },
    "nixpkgs": {
      "locked": {
        "lastModified": 1710272261,
        "narHash": "sha256-g+z7DFEIGGxPcQ4kDsSlFNzXJVhqPiGMrx0cPYrGnNA=",
        "owner": "NixOS",
        "repo": "nixpkgs",
        "rev": "0ad13a6833440b8e238947e47bea7f11071dc2b2",
        "type": "github"
      },
      "original": { "owner": "NixOS", "ref": "nixos-unstable", "repo": "nixpkgs", "type": "github" }
    },
    "root": {
      "inputs": { "flake-compat": "flake-compat", "nixpkgs": "nixpkgs" }
    }
  },
  "root": "root",
  "version": 7
}`

	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, lockContent)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)

	deps, err := nf.GetProjectDependencies()
	require.NoError(t, err)
	// flake-compat (flake: false) should still be included as it has Locked data
	assert.Equal(t, 2, len(deps))

	foundFlakeCompat := false
	for _, dep := range deps {
		if dep.Name == "flake-compat" {
			foundFlakeCompat = true
			assert.Equal(t, "sha256-kvjfFW7WAETZlt09AgDn1MrtKzP7t90Vf7isPyY01aI=", dep.SHA256)
		}
	}
	assert.True(t, foundFlakeCompat, "flake-compat (flake: false) should be in dependencies")
}

func TestNixFlexPackUnsupportedVersion(t *testing.T) {
	lockContent := `{
  "nodes": {
    "root": { "inputs": {} }
  },
  "root": "root",
  "version": 5
}`

	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, lockContent)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err) // Constructor succeeds but warns

	deps, err := nf.GetProjectDependencies()
	require.NoError(t, err)
	assert.Empty(t, deps) // No deps because lock parsing was skipped
}

func TestNixFlexPackErrorHandling(t *testing.T) {
	config := nixflex.NixConfig{
		WorkingDirectory: "/nonexistent/path/to/project",
	}

	_, err := nixflex.NewNixFlexPack(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "flake.nix not found")
}

func TestNixFlexPackScopes(t *testing.T) {
	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, realWorldFlakeLock)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)

	scopes := nf.CalculateScopes()
	assert.Equal(t, []string{"build"}, scopes)
}

func TestNixFlexPackParseDependencyToList(t *testing.T) {
	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, realWorldFlakeLock)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)

	depList := nf.ParseDependencyToList()
	assert.Equal(t, 3, len(depList))

	for _, depID := range depList {
		assert.Contains(t, depID, ":")
	}
}

func TestNixFlexPackGetDependency(t *testing.T) {
	tempDir := t.TempDir()
	writeFlakeNix(t, tempDir)
	writeFlakeLock(t, tempDir, realWorldFlakeLock)

	config := nixflex.NixConfig{
		WorkingDirectory: tempDir,
	}

	nf, err := nixflex.NewNixFlexPack(config)
	require.NoError(t, err)

	depStr := nf.GetDependency()
	assert.Contains(t, depStr, "Project:")
	assert.Contains(t, depStr, "Dependencies:")
}

// writeFlakeNix creates a minimal flake.nix in the given directory.
func writeFlakeNix(t *testing.T, dir string) {
	t.Helper()
	content := `{
  description = "Test flake";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs = { self, nixpkgs }: {};
}`
	err := os.WriteFile(filepath.Join(dir, "flake.nix"), []byte(content), 0644)
	require.NoError(t, err)
}

// writeFlakeLock writes the given lock content to flake.lock in the directory.
func writeFlakeLock(t *testing.T, dir, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, "flake.lock"), []byte(content), 0644)
	require.NoError(t, err)
}
