package nuget

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
)

const (
	nupkgExtension      = ".nupkg"
	snupkgExtension     = ".snupkg"
	legacySymbolsSuffix = ".symbols.nupkg"
	artifactTypeNupkg   = "nupkg"
	artifactTypeSnupkg  = "snupkg"
)

// packageArtifactType returns the build-info artifact type for a NuGet package file,
// or an empty string when the file is not a NuGet package.
//   - "<id>.<version>.snupkg"          -> "snupkg"
//   - "<id>.<version>.symbols.nupkg"   -> "snupkg" (legacy symbol package format)
//   - "<id>.<version>.nupkg"           -> "nupkg"
func packageArtifactType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, snupkgExtension):
		return artifactTypeSnupkg
	case strings.HasSuffix(lower, legacySymbolsSuffix):
		return artifactTypeSnupkg
	case strings.HasSuffix(lower, nupkgExtension):
		return artifactTypeNupkg
	default:
		return ""
	}
}

// isPackageFile reports whether name is a NuGet package (.nupkg/.snupkg/.symbols.nupkg).
func isPackageFile(name string) bool {
	return packageArtifactType(name) != ""
}

// newArtifactFromFile builds an entities.Artifact for a local NuGet package file,
// computing checksums and deriving its deployment path. Artifactory's NuGet push API
// (used by both "dotnet nuget push" and "nuget push") always stores the package flat at
// the repository root as "<file>", regardless of the NuGet client protocol version used
// for restore; there is no "<id>/<version>/<file>" subfolder layout to account for.
// The artifact type is set to "nupkg" or "snupkg".
func newArtifactFromFile(fullPath, repoName string) (entities.Artifact, error) {
	name := filepath.Base(fullPath)
	artifactType := packageArtifactType(name)
	if artifactType == "" {
		return entities.Artifact{}, fmt.Errorf("%s is not a NuGet package file", name)
	}
	details, err := crypto.GetFileDetails(fullPath, true)
	if err != nil {
		return entities.Artifact{}, fmt.Errorf("compute checksum for %s: %w", name, err)
	}
	return entities.Artifact{
		Name:                   name,
		Type:                   artifactType,
		Path:                   name,
		OriginalDeploymentRepo: repoName,
		Checksum: entities.Checksum{
			Sha1:   details.Checksum.Sha1,
			Sha256: details.Checksum.Sha256,
			Md5:    details.Checksum.Md5,
		},
	}, nil
}

// BuildArtifactModules groups uploaded/packed artifacts into build-info modules.
//   - When moduleOverride is set (user-supplied --module), all artifacts are placed in a
//     single module using that ID.
//   - Otherwise each package is placed in a module whose ID is the fixed "<PackageId>:<Version>"
//     derived from the package file name, so a primary package and its symbol package share one
//     module and distinct packages get distinct modules. Insertion order is preserved.
func BuildArtifactModules(artifacts []entities.Artifact, moduleOverride string) []entities.Module {
	if moduleOverride != "" {
		return []entities.Module{{
			Id:        moduleOverride,
			Type:      entities.Nuget,
			Artifacts: artifacts,
		}}
	}
	if len(artifacts) == 0 {
		return nil
	}
	var order []string
	groups := make(map[string][]entities.Artifact)
	for _, a := range artifacts {
		moduleID := moduleIDFromFilename(a.Name)
		if _, seen := groups[moduleID]; !seen {
			order = append(order, moduleID)
		}
		groups[moduleID] = append(groups[moduleID], a)
	}
	modules := make([]entities.Module, 0, len(order))
	for _, id := range order {
		modules = append(modules, entities.Module{
			Id:        id,
			Type:      entities.Nuget,
			Artifacts: groups[id],
		})
	}
	return modules
}

// moduleIDFromFilename returns the fixed "<PackageId>:<Version>" module ID for a package file,
// falling back to the package ID (or the raw file name) when the version cannot be derived.
func moduleIDFromFilename(fileName string) string {
	id, version := parseNupkgFilename(fileName)
	switch {
	case id != "" && version != "":
		return id + ":" + version
	case id != "":
		return id
	default:
		return fileName
	}
}

// FindNupkgArtifacts scans outputDir (non-recursively) for NuGet package files and returns
// entities.Artifact structs with checksums computed from the local files. It recognizes
// primary packages (.nupkg) as well as symbol packages (.snupkg and legacy .symbols.nupkg).
func FindNupkgArtifacts(outputDir, repoName string) ([]entities.Artifact, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, fmt.Errorf("scan nupkg output dir %s: %w", outputDir, err)
	}
	var artifacts []entities.Artifact
	for _, e := range entries {
		if e.IsDir() || !isPackageFile(e.Name()) {
			continue
		}
		artifact, err := newArtifactFromFile(filepath.Join(outputDir, e.Name()), repoName)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

// CollectPushArtifacts resolves the package files that a push command uploads. It considers
// only the explicit positional package arguments (paths or globs) supplied to the native
// command, so it never captures stale, unrelated packages that happen to sit in the working
// directory. Relative arguments are resolved against workingDir. Symbol packages pushed
// alongside the primary package are included when matched by the arguments.
func CollectPushArtifacts(workingDir string, pushArgs []string, repoName string) ([]entities.Artifact, error) {
	paths, err := resolvePushPackagePaths(workingDir, pushArgs)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no NuGet package (.nupkg/.snupkg) found in the push arguments")
	}
	var artifacts []entities.Artifact
	for _, p := range paths {
		artifact, err := newArtifactFromFile(p, repoName)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

// resolvePushPackagePaths extracts package file paths from the positional push arguments,
// expanding globs and ignoring flags and flag values. Results are absolute and de-duplicated.
func resolvePushPackagePaths(workingDir string, pushArgs []string) ([]string, error) {
	seen := make(map[string]bool)
	var paths []string
	for _, arg := range pushArgs {
		// Skip option flags (e.g. --source, -ApiKey) and their inline values.
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// Only positional arguments that look like package files are push targets.
		if !isPackageFile(arg) {
			continue
		}
		candidate := arg
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(workingDir, candidate)
		}
		matches, err := filepath.Glob(candidate)
		if err != nil {
			return nil, fmt.Errorf("resolve push argument %q: %w", arg, err)
		}
		if len(matches) == 0 {
			// Not a glob (or no match); keep the literal path if it exists.
			if _, statErr := os.Stat(candidate); statErr == nil {
				matches = []string{candidate}
			}
		}
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil {
				return nil, fmt.Errorf("resolve push artifact %q: %w", m, err)
			}
			if isPackageFile(abs) && !seen[abs] {
				seen[abs] = true
				paths = append(paths, abs)
			}
		}
	}
	return paths, nil
}

// PackageSnapshot records the package files present under a directory tree, keyed by
// absolute path, with their last-modified time. It is used to detect the packages that a
// pack command produces or refreshes.
type PackageSnapshot map[string]time.Time

// SnapshotPackageFiles walks root recursively and records every existing NuGet package file
// with its modification time. Missing roots yield an empty snapshot rather than an error.
func SnapshotPackageFiles(root string) (PackageSnapshot, error) {
	snapshot := make(PackageSnapshot)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !isPackageFile(info.Name()) {
			return nil
		}
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return absErr
		}
		snapshot[abs] = info.ModTime()
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("snapshot package files under %s: %w", root, err)
	}
	return snapshot, nil
}

// CollectPackedArtifacts returns artifacts for package files under root that are new or were
// modified relative to the "before" snapshot. This deterministically captures the output of a
// pack command (including custom --output directories and bin/<Configuration> defaults) without
// depending on a single directory and without including pre-existing, unrelated packages.
func CollectPackedArtifacts(root string, before PackageSnapshot, repoName string) ([]entities.Artifact, error) {
	after, err := SnapshotPackageFiles(root)
	if err != nil {
		return nil, err
	}
	var artifacts []entities.Artifact
	for path, modTime := range after {
		prev, existed := before[path]
		if existed && !modTime.After(prev) {
			continue
		}
		artifact, err := newArtifactFromFile(path, repoName)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

// parseNupkgFilename extracts PackageId and Version from "<id>.<version>.nupkg",
// "<id>.<version>.snupkg", or the legacy "<id>.<version>.symbols.nupkg".
// NuGet convention: the first dot-separated segment beginning with a digit starts the version.
func parseNupkgFilename(filename string) (pkgID, version string) {
	base := filename
	for _, suffix := range []string{legacySymbolsSuffix, snupkgExtension, nupkgExtension} {
		if strings.HasSuffix(strings.ToLower(base), suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	parts := strings.Split(base, ".")
	for i, p := range parts {
		if len(p) > 0 && p[0] >= '0' && p[0] <= '9' {
			return strings.Join(parts[:i], "."), strings.Join(parts[i:], ".")
		}
	}
	return base, ""
}
