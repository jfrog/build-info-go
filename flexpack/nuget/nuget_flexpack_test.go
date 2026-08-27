package nuget

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jfrog/build-info-go/entities"
)

func TestParseNupkgFilename(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		expectedID  string
		expectedVer string
	}{
		{name: "package", filename: "Newtonsoft.Json.13.0.1.nupkg", expectedID: "Newtonsoft.Json", expectedVer: "13.0.1"},
		{name: "symbol package", filename: "Microsoft.Extensions.DependencyInjection.8.0.0.snupkg", expectedID: "Microsoft.Extensions.DependencyInjection", expectedVer: "8.0.0"},
		{name: "legacy symbol package", filename: "My.Package.1.0.0-preview.1.symbols.nupkg", expectedID: "My.Package", expectedVer: "1.0.0-preview.1"},
		{name: "no version", filename: "SimplePackage.nupkg", expectedID: "SimplePackage"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualID, actualVer := parseNupkgFilename(test.filename)
			if actualID != test.expectedID {
				t.Errorf("package ID: got %q, want %q", actualID, test.expectedID)
			}
			if actualVer != test.expectedVer {
				t.Errorf("version: got %q, want %q", actualVer, test.expectedVer)
			}
		})
	}
}

func TestFindNupkgArtifactsIncludesSymbols(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"My.Package.1.0.0.nupkg",
		"My.Package.1.0.0.snupkg",
		"Legacy.2.1.0.symbols.nupkg",
	} {
		writePackageFile(t, dir, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	artifacts, err := FindNupkgArtifacts(dir, "my-nuget-repo")
	if err != nil {
		t.Fatalf("FindNupkgArtifacts() error = %v", err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(artifacts))
	}

	artifactsByName := make(map[string]entities.Artifact, len(artifacts))
	for _, artifact := range artifacts {
		artifactsByName[artifact.Name] = artifact
		if artifact.OriginalDeploymentRepo != "my-nuget-repo" {
			t.Errorf("repository for %q: got %q", artifact.Name, artifact.OriginalDeploymentRepo)
		}
		if artifact.Sha1 == "" || artifact.Sha256 == "" || artifact.Md5 == "" {
			t.Errorf("expected all checksums for %q", artifact.Name)
		}
	}

	assertArtifact(t, artifactsByName["My.Package.1.0.0.nupkg"], "nupkg", "My.Package.1.0.0.nupkg")
	// .snupkg → symbolpackage/<id>.<version>.nupkg (symbolpackage endpoint).
	assertArtifact(t, artifactsByName["My.Package.1.0.0.snupkg"], "snupkg", "symbolpackage/My.Package.1.0.0.nupkg")
	// .symbols.nupkg (legacy) → flat at root as <id>.<version>.nupkg (regular endpoint).
	assertArtifact(t, artifactsByName["Legacy.2.1.0.symbols.nupkg"], "snupkg", "Legacy.2.1.0.nupkg")
}

func TestCollectPushArtifactsUsesOnlyExplicitArguments(t *testing.T) {
	workingDir := t.TempDir()
	packagesDir := filepath.Join(workingDir, "artifacts")
	writePackageFile(t, packagesDir, "Pushed.1.2.3.nupkg")
	writePackageFile(t, packagesDir, "Pushed.1.2.3.snupkg")
	writePackageFile(t, workingDir, "Stale.9.9.9.nupkg")

	artifacts, err := CollectPushArtifacts(workingDir, []string{
		filepath.Join("artifacts", "*.nupkg"),
		filepath.Join("artifacts", "*.snupkg"),
		"--source",
		"https://example.test/nuget",
	}, "nuget-local")
	if err != nil {
		t.Fatalf("CollectPushArtifacts() error = %v", err)
	}

	actualNames := artifactNames(artifacts)
	expectedNames := []string{"Pushed.1.2.3.nupkg", "Pushed.1.2.3.snupkg"}
	if !equalStrings(actualNames, expectedNames) {
		t.Fatalf("artifact names: got %v, want %v", actualNames, expectedNames)
	}
}

func TestCollectPackedArtifactsFindsNestedAndCustomOutputs(t *testing.T) {
	workingDir := t.TempDir()
	writePackageFile(t, workingDir, filepath.Join("bin", "Debug", "Stale.1.0.0.nupkg"))

	before, err := SnapshotPackageFiles(workingDir)
	if err != nil {
		t.Fatalf("SnapshotPackageFiles() error = %v", err)
	}

	writePackageFile(t, workingDir, filepath.Join("bin", "Release", "Default.Output.2.0.0.nupkg"))
	writePackageFile(t, workingDir, filepath.Join("artifacts", "Custom.Output.3.0.0.snupkg"))

	artifacts, err := CollectPackedArtifacts(workingDir, before, "nuget-local")
	if err != nil {
		t.Fatalf("CollectPackedArtifacts() error = %v", err)
	}

	actualNames := artifactNames(artifacts)
	expectedNames := []string{"Custom.Output.3.0.0.snupkg", "Default.Output.2.0.0.nupkg"}
	if !equalStrings(actualNames, expectedNames) {
		t.Fatalf("artifact names: got %v, want %v", actualNames, expectedNames)
	}
}

func TestBuildArtifactModules(t *testing.T) {
	artifacts := []entities.Artifact{
		{Name: "First.Package.1.0.0.nupkg", Type: "nupkg"},
		{Name: "First.Package.1.0.0.snupkg", Type: "snupkg"},
		{Name: "Second.Package.2.0.0.nupkg", Type: "nupkg"},
	}

	t.Run("default name and version module IDs", func(t *testing.T) {
		modules := BuildArtifactModules(artifacts, "")
		if len(modules) != 2 {
			t.Fatalf("expected 2 modules, got %d", len(modules))
		}
		if modules[0].Id != "First.Package:1.0.0" || len(modules[0].Artifacts) != 2 {
			t.Errorf("unexpected first module: %+v", modules[0])
		}
		if modules[1].Id != "Second.Package:2.0.0" || len(modules[1].Artifacts) != 1 {
			t.Errorf("unexpected second module: %+v", modules[1])
		}
	})

	t.Run("module override groups all artifacts", func(t *testing.T) {
		modules := BuildArtifactModules(artifacts, "custom-module")
		if len(modules) != 1 {
			t.Fatalf("expected 1 module, got %d", len(modules))
		}
		if modules[0].Id != "custom-module" || len(modules[0].Artifacts) != len(artifacts) {
			t.Errorf("unexpected overridden module: %+v", modules[0])
		}
	})
}

func writePackageFile(t *testing.T, root, relativePath string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create package directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(relativePath), 0o600); err != nil {
		t.Fatalf("write package file: %v", err)
	}
}

func artifactNames(artifacts []entities.Artifact) []string {
	names := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		names = append(names, artifact.Name)
	}
	sort.Strings(names)
	return names
}

func assertArtifact(t *testing.T, artifact entities.Artifact, expectedType, expectedPath string) {
	t.Helper()
	if artifact.Type != expectedType {
		t.Errorf("artifact type: got %q, want %q", artifact.Type, expectedType)
	}
	if artifact.Path != expectedPath {
		t.Errorf("artifact path: got %q, want %q", artifact.Path, expectedPath)
	}
}

func equalStrings(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return false
		}
	}
	return true
}
