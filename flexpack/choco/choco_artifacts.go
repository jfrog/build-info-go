package choco

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/crypto"
)

const (
	nupkgExtension = ".nupkg"
	nupkgType      = "nupkg"
)

type snapshotEntry struct {
	modTime time.Time
	sha1    string
}

// PackageSnapshot records .nupkg files in a Chocolatey pack working directory.
type PackageSnapshot map[string]snapshotEntry

// SnapshotPackageFiles records the .nupkg files in root and in each of extraDirs. extraDirs carries
// the directory named by `choco pack --output-directory` (and its aliases): that flag exists, and
// without it a pack into a custom directory is invisible to the snapshot diff, so the build-info
// comes back with no artifacts and no error at all.
func SnapshotPackageFiles(root string, extraDirs ...string) (PackageSnapshot, error) {
	if root == "" {
		return nil, errors.New("the Chocolatey package snapshot root must not be empty")
	}
	snapshot := make(PackageSnapshot)
	for _, directory := range append([]string{root}, extraDirs...) {
		if err := addDirectoryToSnapshot(snapshot, directory, directory == root); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

// addDirectoryToSnapshot indexes one directory. A missing extra directory is not an error: choco
// creates the output directory itself, so it legitimately may not exist when the snapshot is taken.
func addDirectoryToSnapshot(snapshot PackageSnapshot, directory string, required bool) error {
	if directory == "" {
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		if !required && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Chocolatey package directory %s: %w", directory, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !isNupkg(entry.Name()) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat Chocolatey package %s: %w", path, err)
		}
		details, err := crypto.GetFileDetails(path, true)
		if err != nil {
			return fmt.Errorf("compute snapshot checksum for %s: %w", path, err)
		}
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve Chocolatey package path %s: %w", path, err)
		}
		snapshot[absolutePath] = snapshotEntry{modTime: info.ModTime(), sha1: details.Checksum.Sha1}
	}
	return nil
}

// ExtractPackOutputDirectory returns the directory named by `choco pack`'s output-directory flag,
// or "" when the command did not pass one. Chocolatey accepts four spellings, in both the
// "--flag value" and "--flag=value" forms.
func ExtractPackOutputDirectory(args []string) string {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		if !isPackOutputDirectoryFlag(name) {
			continue
		}
		if hasValue {
			return strings.Trim(value, `"'`)
		}
		if index+1 < len(args) {
			return strings.Trim(args[index+1], `"'`)
		}
	}
	return ""
}

func isPackOutputDirectoryFlag(name string) bool {
	switch strings.ToLower(strings.TrimLeft(name, "-")) {
	case "out", "outdir", "outputdirectory", "output-directory":
		return true
	default:
		return false
	}
}

// CollectPackedArtifacts diffs the post-pack state of root (and extraDirs) against before. Pass the
// same extraDirs given to SnapshotPackageFiles, so a pack that wrote to --output-directory is seen.
func CollectPackedArtifacts(root string, before PackageSnapshot, repoName string, extraDirs ...string) ([]entities.Artifact, error) {
	after, err := SnapshotPackageFiles(root, extraDirs...)
	if err != nil {
		return nil, err
	}
	artifacts := make([]entities.Artifact, 0)
	for path, entry := range after {
		previous, existed := before[path]
		if existed && !entry.modTime.After(previous.modTime) && entry.sha1 == previous.sha1 {
			continue
		}
		artifact, err := newArtifactFromFile(path, repoName)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	sort.Slice(artifacts, func(left, right int) bool { return artifacts[left].Name < artifacts[right].Name })
	return artifacts, nil
}

func CollectPushArtifacts(workingDirectory string, args []string, repoName string) ([]entities.Artifact, error) {
	paths, err := resolvePushPackagePaths(workingDirectory, args)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no Chocolatey package (.nupkg) found in the push arguments")
	}
	artifacts := make([]entities.Artifact, 0, len(paths))
	for _, path := range paths {
		artifact, err := newArtifactFromFile(path, repoName)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func BuildArtifactModules(artifacts []entities.Artifact, moduleOverride string) []entities.Module {
	if moduleOverride != "" {
		return []entities.Module{{Id: moduleOverride, Type: entities.Nuget, Artifacts: artifacts}}
	}
	if len(artifacts) == 0 {
		return nil
	}
	groups := make(map[string][]entities.Artifact)
	order := make([]string, 0)
	for _, artifact := range artifacts {
		moduleID := moduleIDFromFilename(artifact.Name)
		if _, exists := groups[moduleID]; !exists {
			order = append(order, moduleID)
		}
		groups[moduleID] = append(groups[moduleID], artifact)
	}
	modules := make([]entities.Module, 0, len(order))
	for _, moduleID := range order {
		modules = append(modules, entities.Module{Id: moduleID, Type: entities.Nuget, Artifacts: groups[moduleID]})
	}
	return modules
}

func newArtifactFromFile(path, repoName string) (entities.Artifact, error) {
	name := filepath.Base(path)
	if !isNupkg(name) {
		return entities.Artifact{}, fmt.Errorf("%s is not a Chocolatey package", name)
	}
	details, err := crypto.GetFileDetails(path, true)
	if err != nil {
		return entities.Artifact{}, fmt.Errorf("compute checksum for %s: %w", name, err)
	}
	return entities.Artifact{
		Name: name, Type: nupkgType, Path: name, OriginalDeploymentRepo: repoName,
		Checksum: entities.Checksum{Sha1: details.Checksum.Sha1, Sha256: details.Checksum.Sha256, Md5: details.Checksum.Md5},
	}, nil
}

func resolvePushPackagePaths(workingDirectory string, args []string) ([]string, error) {
	seen := make(map[string]bool)
	paths := make([]string, 0)
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && chocoOptionTakesValue(arg) {
				skipNext = true
			}
			continue
		}
		if !isNupkg(arg) {
			continue
		}
		candidate := arg
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(workingDirectory, candidate)
		}
		matches, err := filepath.Glob(candidate)
		if err != nil {
			return nil, fmt.Errorf("resolve Chocolatey push argument %q: %w", arg, err)
		}
		if len(matches) == 0 {
			matches = []string{candidate}
		}
		for _, match := range matches {
			absolutePath, err := filepath.Abs(match)
			if err != nil {
				return nil, err
			}
			if _, err = os.Stat(absolutePath); err == nil && !seen[absolutePath] {
				seen[absolutePath] = true
				paths = append(paths, absolutePath)
			}
		}
	}
	if len(paths) == 0 {
		return packagePathFromWorkingDirectory(workingDirectory)
	}
	return paths, nil
}

// packagePathFromWorkingDirectory reproduces Chocolatey's own behaviour when `choco push` is given
// no path: it pushes the single .nupkg in the current folder, and requires an explicit path only
// when there is more than one. Without this, a bare `jf choco push` uploads the package but records
// no artifact, leaving it unstamped and absent from the build-info.
func packagePathFromWorkingDirectory(workingDirectory string) ([]string, error) {
	if workingDirectory == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(workingDirectory, "*"+nupkgExtension))
	if err != nil {
		return nil, fmt.Errorf("look for a Chocolatey package in %s: %w", workingDirectory, err)
	}
	if len(matches) != 1 {
		// Zero means nothing was pushed from here; more than one means Chocolatey itself demanded
		// an explicit path, so the push did not happen. Neither case is an error to report here.
		return nil, nil
	}
	absolutePath, err := filepath.Abs(matches[0])
	if err != nil {
		return nil, err
	}
	return []string{absolutePath}, nil
}

// chocoOptionTakesValue reports whether a Chocolatey option consumes the following argument as its
// value, so that value is not mistaken for a package ID or a .nupkg path.
//
// "-v" is deliberately absent: it is Chocolatey's global --verbose boolean switch, not a short form
// of --version. ChocolateyInstallCommand registers the version option as "version=" with no "v|"
// alias. Treating "-v" as value-taking made "choco install -v pkgname" swallow "pkgname", so the
// build-info recorded no dependencies at all.
func chocoOptionTakesValue(option string) bool {
	switch strings.ToLower(option) {
	case "-s", "--source", "--version", "--package-parameters", "--install-arguments",
		"--execution-timeout", "--cache-location", "--proxy", "--proxy-user", "--proxy-password",
		"--cert", "--certpassword",
		// `choco pack` output-directory spellings: their value is a directory, never a package.
		"--out", "--outdir", "--outputdirectory", "--output-directory",
		// `choco push` credential spellings, so a key is never treated as a package path.
		"-k", "--key", "--apikey", "--api-key":
		return true
	default:
		return false
	}
}

func isNupkg(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), nupkgExtension)
}

func parseNupkgFilename(filename string) (string, string) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	parts := strings.Split(base, ".")
	for index, part := range parts {
		if index > 0 && len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
			return strings.Join(parts[:index], "."), strings.Join(parts[index:], ".")
		}
	}
	return base, ""
}

func moduleIDFromFilename(filename string) string {
	packageID, version := parseNupkgFilename(filename)
	if version == "" {
		return packageID
	}
	return packageID + ":" + version
}
