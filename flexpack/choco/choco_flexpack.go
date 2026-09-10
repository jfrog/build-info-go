package choco

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jfrog/build-info-go/entities"
	buildinfoflex "github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/crypto"
)

// ChocoFlexPack collects the Chocolatey dependencies of an install or upgrade - the requested
// packages and their transitives - from Chocolatey's lib directory.
type ChocoFlexPack struct {
	config buildinfoflex.ChocoConfig
	log    utils.Log
}

func NewChocoFlexPack(config buildinfoflex.ChocoConfig, log utils.Log) (*ChocoFlexPack, error) {
	if config.WorkingDirectory == "" {
		return nil, fmt.Errorf("ChocoConfig.WorkingDirectory must not be empty")
	}
	if log == nil {
		log = utils.NewDefaultLogger(utils.INFO)
	}
	return &ChocoFlexPack{config: config, log: log}, nil
}

func (collector *ChocoFlexPack) CollectBuildInfo(buildName, buildNumber string) (*entities.BuildInfo, error) {
	rootModule := collector.config.Module
	if rootModule == "" {
		rootModule = filepath.Base(collector.config.WorkingDirectory)
	}
	dependencies, missing, _, err := collectDependencyTree(collector.config, rootModule, collector.log)
	if err != nil {
		return nil, err
	}
	for _, packageID := range missing {
		collector.log.Warn("Chocolatey package was not found under the lib directory, so it is not" +
			" recorded in the build-info: " + packageID)
	}
	return &entities.BuildInfo{
		Name:   buildName,
		Number: buildNumber,
		Modules: []entities.Module{{
			Id:           rootModule,
			Type:         entities.Nuget,
			Dependencies: dependencies,
		}},
	}, nil
}

func (collector *ChocoFlexPack) GetProjectDependencies() ([]buildinfoflex.DependencyInfo, error) {
	rootModule := collector.config.Module
	if rootModule == "" {
		rootModule = filepath.Base(collector.config.WorkingDirectory)
	}
	dependencies, _, _, err := collectDependencyTree(collector.config, rootModule, collector.log)
	if err != nil {
		return nil, err
	}
	result := make([]buildinfoflex.DependencyInfo, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, buildinfoflex.DependencyInfo{
			ID: dependency.Id, Type: dependency.Type, SHA1: dependency.Sha1, SHA256: dependency.Sha256,
			MD5: dependency.Md5, Scopes: dependency.Scopes, RequestedBy: dependency.RequestedBy,
			Repository: dependency.Repository,
		})
	}
	return result, nil
}

// GetDependencyGraph returns the dependency graph keyed by module ID: the root module maps to the
// packages named on the command line, and every package maps to the dependencies its .nuspec
// declares that Chocolatey actually installed.
func (collector *ChocoFlexPack) GetDependencyGraph() (map[string][]string, error) {
	rootModule := collector.config.Module
	if rootModule == "" {
		rootModule = filepath.Base(collector.config.WorkingDirectory)
	}
	_, _, graph, err := collectDependencyTree(collector.config, rootModule, collector.log)
	if err != nil {
		return nil, err
	}
	return graph, nil
}

// packageNode is one package located under Chocolatey's lib directory during the dependency walk.
type packageNode struct {
	id          string // package ID with the casing Chocolatey installed it under
	version     string // version taken from the installed .nupkg, not from the declared range
	packagePath string
	requestedBy [][]string
}

// collectDependencyTree resolves the full dependency set of this invocation - the packages named on
// the command line and everything they pull in - entirely from the local Chocolatey installation.
//
// Chocolatey has no lock file, but it does not need one: it installs the fully resolved dependency
// set into $ChocolateyInstall\lib\<id>\, one directory per package, each holding the package's
// .nupkg and .nuspec (NugetService.cs calls GetInstalledPath(packageDependencyInfo), so transitives
// land in the same tree as the requested packages). So the graph is recovered by walking the
// declared <dependencies> of each installed .nuspec and resolving every edge back to the lib
// directory. The declared version is a range, so it is deliberately ignored: the concrete version
// always comes from the installed .nupkg filename.
//
// The walk starts from the command-line packages rather than from a before/after diff of lib, which
// would both over-report (unrelated concurrent installs) and under-report (a transitive already
// satisfied on this machine is still a dependency of this build).
//
// Returns the dependencies in breadth-first order, the IDs that could not be located, and the
// module dependency graph.
func collectDependencyTree(config buildinfoflex.ChocoConfig, rootModule string, log utils.Log) ([]entities.Dependency, []string, map[string][]string, error) {
	root := chocolateyInstallRoot(config.ChocolateyInstall)
	graph := map[string][]string{rootModule: {}}
	missing := make([]string, 0)
	missingSeen := make(map[string]bool)
	ordered := make([]*packageNode, 0, len(config.Packages))
	visited := make(map[string]*packageNode)

	type pendingPackage struct {
		packageID string
		// Path from the requester up to the root module, in build-info RequestedBy order.
		requestedBy []string
	}
	queue := make([]pendingPackage, 0, len(config.Packages))
	for _, packageID := range requestedPackages(config.Packages) {
		queue = append(queue, pendingPackage{packageID: packageID, requestedBy: []string{rootModule}})
	}

	for len(queue) > 0 {
		pending := queue[0]
		queue = queue[1:]
		packagePath, packageName, version, err := resolveInstalledPackage(root, pending.packageID)
		if err != nil {
			if os.IsNotExist(err) {
				// Not under lib. Either the install failed, or the dependency is served by one of
				// Chocolatey's special sources (ruby, cygwin, python, windowsfeatures), which do
				// not produce a .nupkg there at all.
				if key := strings.ToLower(pending.packageID); !missingSeen[key] {
					missingSeen[key] = true
					missing = append(missing, pending.packageID)
				}
				continue
			}
			return nil, nil, nil, err
		}
		dependencyID := packageName + ":" + version
		requester := pending.requestedBy[0]
		graph[requester] = appendUniqueEdge(graph[requester], dependencyID)

		if existing, seen := visited[strings.ToLower(packageName)]; seen {
			// Reached again through a different parent: a shared dependency has several requesters,
			// so record the extra path but do not walk its own dependencies twice. This is also
			// what terminates a dependency cycle.
			existing.requestedBy = append(existing.requestedBy, pending.requestedBy)
			continue
		}
		node := &packageNode{
			id: packageName, version: version, packagePath: packagePath,
			requestedBy: [][]string{pending.requestedBy},
		}
		visited[strings.ToLower(packageName)] = node
		ordered = append(ordered, node)
		if _, exists := graph[dependencyID]; !exists {
			graph[dependencyID] = []string{}
		}

		declared, err := readNuspecDependencies(filepath.Dir(packagePath), packageName, log)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, declaredID := range declared {
			// A fresh slice per child: the paths are retained in the build-info, so they must not
			// share a backing array with a sibling's path.
			childRequestedBy := append([]string{dependencyID}, pending.requestedBy...)
			queue = append(queue, pendingPackage{packageID: declaredID, requestedBy: childRequestedBy})
		}
	}

	dependencies := make([]entities.Dependency, 0, len(ordered))
	for _, node := range ordered {
		details, err := crypto.GetFileDetails(node.packagePath, true)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("compute dependency checksum for %s: %w", node.packagePath, err)
		}
		dependencies = append(dependencies, entities.Dependency{
			Id:          node.id + ":" + node.version,
			Type:        nupkgType,
			Scopes:      []string{"compile"},
			RequestedBy: node.requestedBy,
			Repository:  config.RepoResolve,
			Checksum: entities.Checksum{
				Sha1: details.Checksum.Sha1, Sha256: details.Checksum.Sha256, Md5: details.Checksum.Md5,
			},
		})
	}
	return dependencies, missing, graph, nil
}

func appendUniqueEdge(edges []string, dependencyID string) []string {
	for _, existing := range edges {
		if existing == dependencyID {
			return edges
		}
	}
	return append(edges, dependencyID)
}

// nuspecDocument is the subset of the NuGet .nuspec schema needed to read declared dependencies.
// The dependency element is either listed directly or grouped per target framework; Chocolatey
// packages use the flat form, but the grouped form is valid and is read too. Field tags carry no
// namespace, so they match the .nuspec regardless of which schema revision it declares.
type nuspecDocument struct {
	Metadata struct {
		ID           string `xml:"id"`
		Version      string `xml:"version"`
		Dependencies struct {
			Dependencies []nuspecDependency `xml:"dependency"`
			Groups       []struct {
				Dependencies []nuspecDependency `xml:"dependency"`
			} `xml:"group"`
		} `xml:"dependencies"`
	} `xml:"metadata"`
}

type nuspecDependency struct {
	ID string `xml:"id,attr"`
}

// readNuspecDependencies returns the package IDs declared by the .nuspec in an installed package
// directory. A package with no .nuspec, or with an unreadable one, is treated as a leaf: a manifest
// problem in an already-installed dependency must not fail the build-info collection.
func readNuspecDependencies(packageDirectory, packageName string, log utils.Log) ([]string, error) {
	nuspecPath, err := findNuspec(packageDirectory, packageName)
	if err != nil {
		return nil, err
	}
	if nuspecPath == "" {
		log.Debug("No .nuspec found for Chocolatey package " + packageName + " in " + packageDirectory +
			"; treating it as having no dependencies.")
		return nil, nil
	}
	content, err := os.ReadFile(nuspecPath)
	if err != nil {
		return nil, fmt.Errorf("read Chocolatey manifest %s: %w", nuspecPath, err)
	}
	var document nuspecDocument
	if err := xml.Unmarshal(content, &document); err != nil {
		log.Warn("Could not parse the Chocolatey manifest " + nuspecPath + ", so the dependencies of " +
			packageName + " are not recorded in the build-info: " + err.Error())
		// Swallowing the error is deliberate: an unparseable manifest in an already-installed
		// dependency means its own dependencies are unknown, not that the build failed. Returning
		// the error here would fail build-info collection for the whole command over one bad
		// package, so the package is treated as a leaf and the warning above is the record.
		//nolint:nilerr // see above: a malformed manifest degrades to a leaf, it does not fail
		return nil, nil
	}
	declared := document.Metadata.Dependencies.Dependencies
	for _, group := range document.Metadata.Dependencies.Groups {
		declared = append(declared, group.Dependencies...)
	}
	result := make([]string, 0, len(declared))
	seen := make(map[string]bool)
	for _, dependency := range declared {
		id := strings.TrimSpace(dependency.ID)
		if id == "" || seen[strings.ToLower(id)] {
			continue
		}
		seen[strings.ToLower(id)] = true
		result = append(result, id)
	}
	return result, nil
}

// findNuspec locates the manifest inside an installed package directory, returning "" when there is
// none. Chocolatey repairs the manifest's casing after install (NugetService.NormalizeNuspecCasing),
// but feeds have shipped it lower-cased, so the name is matched case-insensitively.
func findNuspec(packageDirectory, packageName string) (string, error) {
	entries, err := os.ReadDir(packageDirectory)
	if err != nil {
		return "", fmt.Errorf("read Chocolatey package directory %s: %w", packageDirectory, err)
	}
	candidate := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".nuspec") {
			continue
		}
		if strings.EqualFold(entry.Name(), packageName+".nuspec") {
			return filepath.Join(packageDirectory, entry.Name()), nil
		}
		if candidate == "" {
			candidate = filepath.Join(packageDirectory, entry.Name())
		}
	}
	return candidate, nil
}

func resolveInstalledPackage(chocolateyRoot, requestedPackage string) (string, string, string, error) {
	libraryRoot := filepath.Join(chocolateyRoot, "lib")
	entries, err := os.ReadDir(libraryRoot)
	if err != nil {
		return "", "", "", err
	}
	packageDirectory := ""
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), requestedPackage) {
			packageDirectory = filepath.Join(libraryRoot, entry.Name())
			break
		}
	}
	if packageDirectory == "" {
		return "", "", "", os.ErrNotExist
	}
	entries, err = os.ReadDir(packageDirectory)
	if err != nil {
		return "", "", "", err
	}
	var selectedPath string
	var selectedTime time.Time
	var selectedName, selectedVersion string
	for _, entry := range entries {
		if entry.IsDir() || !isNupkg(entry.Name()) {
			continue
		}
		// Chocolatey stores the package as lib\<id>\<id>.nupkg, with NO version in the file name
		// (verified against chocolatey/choco's own integration suite, e.g.
		// Path.Combine(..., "lib", "isdependency", "isdependency.nupkg")). parseNupkgFilename
		// therefore returns an empty version here in normal operation, and the real version comes
		// from the sibling .nuspec below. Feeds and older clients have shipped the versioned form
		// too, so a version parsed off the name is still accepted when present.
		name, version := parseNupkgFilename(entry.Name())
		if !strings.EqualFold(name, requestedPackage) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", "", "", err
		}
		if selectedPath == "" || info.ModTime().After(selectedTime) {
			selectedPath = filepath.Join(packageDirectory, entry.Name())
			selectedTime = info.ModTime()
			selectedName, selectedVersion = name, version
		}
	}
	if selectedPath == "" {
		return "", "", "", os.ErrNotExist
	}
	if selectedVersion == "" {
		selectedVersion = readNuspecVersion(packageDirectory, requestedPackage)
		if selectedVersion == "" {
			return "", "", "", fmt.Errorf(
				"could not determine the installed version of Chocolatey package %s: %s holds no versioned .nupkg and no readable .nuspec",
				requestedPackage, packageDirectory)
		}
	}
	return selectedPath, selectedName, selectedVersion, nil
}

// readNuspecVersion reads <metadata><version> from an installed package's manifest, which is where
// the version lives once Chocolatey has extracted the package: the .nupkg beside it is named
// <id>.nupkg with no version. Returns "" when the manifest is absent or unparseable, leaving the
// caller to report a resolution failure naming the package.
func readNuspecVersion(packageDirectory, packageName string) string {
	nuspecPath, err := findNuspec(packageDirectory, packageName)
	if err != nil || nuspecPath == "" {
		return ""
	}
	content, err := os.ReadFile(nuspecPath)
	if err != nil {
		return ""
	}
	var document nuspecDocument
	if err := xml.Unmarshal(content, &document); err != nil {
		return ""
	}
	return strings.TrimSpace(document.Metadata.Version)
}

func chocolateyInstallRoot(configuredRoot string) string {
	if configuredRoot != "" {
		return configuredRoot
	}
	if root := os.Getenv("ChocolateyInstall"); root != "" {
		return root
	}
	if programData := os.Getenv("ProgramData"); programData != "" {
		return filepath.Join(programData, "chocolatey")
	}
	return `C:\ProgramData\chocolatey`
}

func requestedPackages(args []string) []string {
	result := make([]string, 0)
	seen := make(map[string]bool)
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "" || strings.HasPrefix(arg, "-") {
			if strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") && chocoOptionTakesValue(arg) {
				skipNext = true
			}
			continue
		}
		key := strings.ToLower(arg)
		if !seen[key] {
			seen[key] = true
			result = append(result, arg)
		}
	}
	return result
}
