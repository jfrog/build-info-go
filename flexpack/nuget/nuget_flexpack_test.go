package nuget

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseNupkgFilename(t *testing.T) {
	cases := []struct {
		in      string
		wantID  string
		wantVer string
	}{
		{"Newtonsoft.Json.13.0.1.nupkg", "Newtonsoft.Json", "13.0.1"},
		{"Microsoft.Extensions.DependencyInjection.8.0.0.nupkg", "Microsoft.Extensions.DependencyInjection", "8.0.0"},
		{"My.Package.1.0.0-preview.1.nupkg", "My.Package", "1.0.0-preview.1"},
		{"SimplePackage.2.3.4.nupkg", "SimplePackage", "2.3.4"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			gotID, gotVer := parseNupkgFilename(tc.in)
			if gotID != tc.wantID {
				t.Errorf("pkgID: got %q, want %q", gotID, tc.wantID)
			}
			if gotVer != tc.wantVer {
				t.Errorf("version: got %q, want %q", gotVer, tc.wantVer)
			}
		})
	}
}

func TestFindNupkgArtifacts(t *testing.T) {
	dir := t.TempDir()
	// Create test .nupkg files
	for _, name := range []string{"My.Package.1.0.0.nupkg", "Other.2.1.0.nupkg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fake nupkg content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Create non-nupkg file that should be ignored
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	artifacts, err := FindNupkgArtifacts(dir, "my-nuget-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(artifacts))
	}
	for _, a := range artifacts {
		if a.Type != "nupkg" {
			t.Errorf("artifact type: got %q, want %q", a.Type, "nupkg")
		}
		if a.OriginalDeploymentRepo != "my-nuget-repo" {
			t.Errorf("repo: got %q, want %q", a.OriginalDeploymentRepo, "my-nuget-repo")
		}
		if a.Checksum.Sha1 == "" {
			t.Error("Sha1 must not be empty")
		}
	}
}
