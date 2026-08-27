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

// snapshotEntry records the mtime and SHA1 of a package file at snapshot time.
// Both fields are used for change detection: mtime is the fast path; SHA1 is the
// fallback for filesystems with coarse-grained mtime resolution (FAT32 = 1 s, some SMB mounts).
type snapshotEntry struct {
	ModTime time.Time
	Sha1    string
}

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
// computing checksums and deriving its Artifactory storage path.
//
// Primary packages (.nupkg) are stored flat at the repository root: "<id>.<version>.nupkg".
//
// Modern symbol packages (.snupkg) are pushed via the /api/nuget/v2/<repo>/symbolpackage
// endpoint and stored as: "symbolpackage/<id>.<version>.nupkg".
//
// Legacy symbol packages (.symbols.nupkg) are pushed via the regular package endpoint and
// stored flat at the repository root as "<id>.<version>.nupkg" (extension renamed by Artifactory
// based on nuspec content; the ".symbols" segment is dropped).
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
	lower := strings.ToLower(name)
	var path string
	switch {
	case strings.HasSuffix(lower, snupkgExtension):
		// .snupkg → symbolpackage/<id>.<version>.nupkg
		path = "symbolpackage/" + snupkgStorageName(name)
	case strings.HasSuffix(lower, legacySymbolsSuffix):
		// .symbols.nupkg → flat at root as <id>.<version>.nupkg
		path = snupkgStorageName(name)
	default:
		path = name
	}
	return entities.Artifact{
		Name:                   name,
		Type:                   artifactType,
		Path:                   path,
		OriginalDeploymentRepo: repoName,
		Checksum: entities.Checksum{
			Sha1:   details.Checksum.Sha1,
			Sha256: details.Checksum.Sha256,
			Md5:    details.Checksum.Md5,
		},
	}, nil
}

// snupkgStorageName converts a symbol package filename to the name Artifactory uses when
// storing it: the .snupkg or .symbols.nupkg extension is replaced with .nupkg.
// E.g. "Foo.1.0.0.snupkg" → "Foo.1.0.0.nupkg", "Foo.1.0.0.symbols.nupkg" → "Foo.1.0.0.nupkg".
func snupkgStorageName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, snupkgExtension):
		return name[:len(name)-len(snupkgExtension)] + nupkgExtension
	case strings.HasSuffix(lower, legacySymbolsSuffix):
		return name[:len(name)-len(legacySymbolsSuffix)] + nupkgExtension
	default:
		return name
	}
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
// absolute path. It is used to detect the packages that a pack command produces or refreshes.
// Both ModTime and Sha1 are recorded so that a re-pack that lands within the filesystem's
// mtime resolution (FAT32 = 1 s, some SMB mounts) is still detected via content change.
type PackageSnapshot map[string]snapshotEntry

// SnapshotPackageFiles records every existing NuGet package file in the standard output
// directories (bin/, obj/, artifacts/) under root, plus any additional directories supplied
// in extraDirs. Scoping to known output directories avoids walking the entire source tree,
// which may contain large numbers of source and cache files. Missing directories yield an
// empty snapshot rather than an error.
func SnapshotPackageFiles(root string, extraDirs ...string) (PackageSnapshot, error) {
	snapshot := make(PackageSnapshot)
	dirs := []string{
		filepath.Join(root, "bin"),
		filepath.Join(root, "obj"),
		filepath.Join(root, "artifacts"),
	}
	dirs = append(dirs, extraDirs...)
	for _, dir := range dirs {
		if err := snapshotDir(snapshot, dir); err != nil {
			return nil, fmt.Errorf("snapshot package files under %s: %w", dir, err)
		}
	}
	return snapshot, nil
}

// snapshotDir walks dir and records package files into snapshot. Missing dirs are skipped.
func snapshotDir(snapshot PackageSnapshot, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if info.IsDir() || !isPackageFile(info.Name()) {
			return nil
		}
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return absErr
		}
		details, detailsErr := crypto.GetFileDetails(abs, true)
		sha1 := ""
		if detailsErr == nil {
			sha1 = details.Checksum.Sha1
		}
		snapshot[abs] = snapshotEntry{ModTime: info.ModTime(), Sha1: sha1}
		return nil
	})
}

// CollectPackedArtifacts returns artifacts for package files that are new or were modified
// relative to the "before" snapshot. It scans the same set of directories as the "before"
// snapshot: the standard output directories (bin/, obj/, artifacts/) under root plus any
// extraDirs (e.g. a custom --output directory passed to the pack command).
func CollectPackedArtifacts(root string, before PackageSnapshot, repoName string, extraDirs ...string) ([]entities.Artifact, error) {
	after, err := SnapshotPackageFiles(root, extraDirs...)
	if err != nil {
		return nil, err
	}
	var artifacts []entities.Artifact
	for path, entry := range after {
		prev, existed := before[path]
		if existed && !entry.ModTime.After(prev.ModTime) && entry.Sha1 == prev.Sha1 {
			// Skip: same or older mtime AND same content — not a new or re-packed file.
			// Checking Sha1 catches re-packs on filesystems with coarse mtime resolution.
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
