package flexpack

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

var (
	gradleVersionRegex = regexp.MustCompile(`Gradle\s+(\d+\.\d+(?:\.\d+)?)`)
)

type GradleDepNode struct {
	Group      string
	Module     string
	Version    string
	Classifier string
	Type       string
	Reason     string
	Children   []GradleDepNode
}

type gradleNodePtr struct {
	Group      string
	Module     string
	Version    string
	Classifier string
	Type       string
	Children   []*gradleNodePtr
}

func (gf *GradleFlexPack) parseWithGradleDependencies(moduleName string) {
	isAndroid := false
	if content, _, err := gf.getBuildFileContent(moduleName); err == nil {
		isAndroid = strings.Contains(string(content), "com.android.application") ||
			strings.Contains(string(content), "com.android.library") ||
			strings.Contains(string(content), "android") // Heuristic for 'android { ... }' block or plugin
	}

	var configs []string
	if isAndroid {
		configs = []string{
			"debugCompileClasspath", "debugRuntimeClasspath",
			"releaseCompileClasspath", "releaseRuntimeClasspath",
		}
		if gf.config.IncludeTestDependencies {
			configs = append(configs,
				"debugUnitTestCompileClasspath", "debugUnitTestRuntimeClasspath",
				"releaseUnitTestCompileClasspath", "releaseUnitTestRuntimeClasspath",
				"debugAndroidTestCompileClasspath", "debugAndroidTestRuntimeClasspath",
			)
		}
	} else {
		configs = []string{"compileClasspath", "runtimeClasspath"}
		if gf.config.IncludeTestDependencies {
			configs = append(configs, "testCompileClasspath", "testRuntimeClasspath")
		}
	}

	allDeps := make(map[string]flexpack.DependencyInfo)

	for _, config := range configs {

		taskPrefix := ""
		if moduleName != "" {
			taskPrefix = ":" + moduleName + ":"
		}
		args := []string{taskPrefix + "dependencies", "--configuration", config, "--quiet"}
		output, err := gf.runGradleCommand(args...)
		if err != nil {
			log.Debug(fmt.Sprintf("Failed to get dependencies for configuration %s: %s", config, string(output)))
			continue
		}

		dependencies, err := gf.ParseGradleDependencyTree(string(output))
		if err != nil {
			log.Debug(fmt.Sprintf("Failed to parse text output for configuration %s: %s", config, err.Error()))
			continue
		}

		scopes := gf.MapGradleConfigurationToScopes(config)
		for _, dep := range dependencies {
			gf.processGradleDependency(dep, "", scopes, allDeps)
		}
	}

	for _, dep := range allDeps {
		gf.dependencies = append(gf.dependencies, dep)
	}
	log.Debug(fmt.Sprintf("Collected %d dependencies", len(gf.dependencies)))
}

func (gf *GradleFlexPack) ParseGradleDependencyTree(output string) ([]GradleDepNode, error) {
	lines := strings.Split(output, "\n")
	var roots []*gradleNodePtr
	var stack []*gradleNodePtr
	skippedDepth := -1

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		depth, content := gf.extractDepthAndContent(line)
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}

		if skippedDepth != -1 {
			if depth > skippedDepth {
				continue
			}
			skippedDepth = -1
		}

		// Check if we should omit this dependency
		shouldOmit := false
		if strings.HasSuffix(content, " (c)") || strings.HasSuffix(content, " (n)") {
			shouldOmit = true
		} else if strings.HasSuffix(content, " (*)") {
			withoutStar := strings.TrimSuffix(content, " (*)")
			if strings.HasSuffix(withoutStar, " (c)") || strings.HasSuffix(withoutStar, " (n)") {
				shouldOmit = true
			}
		}

		if shouldOmit {
			skippedDepth = depth
			continue
		}

		node := gf.parseGradleLine(content)
		if node == nil {
			continue
		}

		ptrNode := &gradleNodePtr{
			Group:      node.Group,
			Module:     node.Module,
			Version:    node.Version,
			Classifier: node.Classifier,
			Type:       node.Type,
		}

		if depth == 0 {
			roots = append(roots, ptrNode)
			stack = []*gradleNodePtr{ptrNode}
		} else {
			parent := gf.findParentForDepth(stack, depth)
			if parent != nil {
				parent.Children = append(parent.Children, ptrNode)
			} else {
				// Fallback: treat as root
				log.Debug(fmt.Sprintf("No parent found for dependency at depth %d: %s", depth, content))
				roots = append(roots, ptrNode)
			}
			// Ensure stack size
			if len(stack) <= depth {
				stack = append(stack, make([]*gradleNodePtr, depth-len(stack)+1)...)
			}
			stack[depth] = ptrNode
		}
	}
	return gf.convertNodes(roots), nil
}

func (gf *GradleFlexPack) extractDepthAndContent(line string) (int, string) {
	markerIdx := strings.Index(line, "+--- ")
	if markerIdx == -1 {
		markerIdx = strings.Index(line, "\\--- ")
	}
	if markerIdx == -1 {
		return 0, ""
	}
	depth := gf.calculateTreeDepth(line)
	content := line[markerIdx+5:]
	return depth, content
}

func (gf *GradleFlexPack) calculateTreeDepth(line string) int {
	depth := 0
	i := 0
	for i < len(line) {
		remaining := line[i:]
		if strings.HasPrefix(remaining, "|    ") || strings.HasPrefix(remaining, "     ") {
			depth++
			i += 5
			continue
		}

		// Found tree marker (+--- or \---) or unknown pattern, stop counting depth
		break
	}
	return depth
}

func (gf *GradleFlexPack) parseGradleLine(content string) *GradleDepNode {
	content = strings.TrimSpace(content)
	// Remove constraint markers like (*). (c) and (n) are omitted before calling this function.
	content = strings.TrimSuffix(content, " (*)")

	// Extract type if specified with @type suffix (e.g., @aar, @jar)
	depType := "jar"
	if atIdx := strings.LastIndex(content, "@"); atIdx != -1 {
		depType = content[atIdx+1:]
		content = content[:atIdx]
	}

	// Handle " -> version" resolution (version conflict resolution)
	// Example: group:module:1.0 -> 1.1 or group:module -> 1.1
	var resolvedVersion string
	if arrowIdx := strings.Index(content, " -> "); arrowIdx != -1 {
		resolvedVersion = strings.TrimSpace(content[arrowIdx+4:])
		content = strings.TrimSpace(content[:arrowIdx])
	}

	// Project dependency: project :module
	if strings.HasPrefix(content, "project ") {
		// Extract module name from "project :path:to:module"
		path := strings.TrimPrefix(content, "project ")
		path = strings.TrimPrefix(path, ":")

		// Handle paths like "libs:mylib" -> module is "mylib"
		pathParts := strings.Split(path, ":")
		moduleName := pathParts[len(pathParts)-1]

		group := gf.groupId
		version := gf.projectVersion

		if meta, ok := gf.modulesMap[path]; ok {
			group = meta.Group
			version = meta.Version
			moduleName = meta.Artifact
		}

		return &GradleDepNode{
			Group:   group,
			Module:  moduleName,
			Version: version,
			Type:    depType,
		}
	}

	// format: group:module:version[:classifier]
	parts := strings.Split(content, ":")
	if len(parts) < 2 {
		return nil
	}

	node := &GradleDepNode{
		Group:  parts[0],
		Module: parts[1],
		Type:   depType,
	}

	switch len(parts) {
	case 2:
		if resolvedVersion != "" {
			node.Version = resolvedVersion
		} else {
			return nil
		}
	case 3:
		node.Version = parts[2]
	case 4:
		node.Version = parts[2]
		node.Classifier = parts[3]
	default:
		node.Version = parts[2]
		node.Classifier = parts[3]
	}

	if resolvedVersion != "" {
		node.Version = resolvedVersion
	}
	return node
}

func (gf *GradleFlexPack) findParentForDepth(stack []*gradleNodePtr, depth int) *gradleNodePtr {
	for parentDepth := depth - 1; parentDepth >= 0; parentDepth-- {
		if parentDepth < len(stack) && stack[parentDepth] != nil {
			return stack[parentDepth]
		}
	}
	return nil
}

func (gf *GradleFlexPack) convertNodes(ptrNodes []*gradleNodePtr) []GradleDepNode {
	var nodes []GradleDepNode
	for _, ptr := range ptrNodes {
		node := GradleDepNode{
			Group:      ptr.Group,
			Module:     ptr.Module,
			Version:    ptr.Version,
			Classifier: ptr.Classifier,
			Type:       ptr.Type,
		}
		node.Children = gf.convertNodes(ptr.Children)
		nodes = append(nodes, node)
	}
	return nodes
}

func (gf *GradleFlexPack) MapGradleConfigurationToScopes(config string) []string {
	configLower := strings.ToLower(config)

	switch {
	case strings.Contains(configLower, "testcompileclasspath") || strings.Contains(configLower, "testcompileonly") || strings.Contains(configLower, "testimplementation") ||
		strings.Contains(configLower, "androidtest") || strings.Contains(configLower, "unittest"):
		return []string{"test"}
	case strings.Contains(configLower, "testruntimeclasspath") || strings.Contains(configLower, "testruntimeonly"):
		return []string{"test"}
	case strings.Contains(configLower, "compileclasspath") || strings.Contains(configLower, "compileonly") || configLower == "api" || configLower == "compile":
		return []string{"compile"}
	case strings.Contains(configLower, "runtimeclasspath") || strings.Contains(configLower, "runtimeonly") || configLower == "runtime":
		return []string{"runtime"}
	case strings.Contains(configLower, "provided"):
		return []string{"provided"}
	default:
		return []string{"compile"}
	}
}

func (gf *GradleFlexPack) processGradleDependency(dep GradleDepNode, parent string, scopes []string, allDeps map[string]flexpack.DependencyInfo) {
	if dep.Group == "" || dep.Module == "" || dep.Version == "" {
		return
	}

	var dependencyId string
	if dep.Classifier != "" {
		dependencyId = fmt.Sprintf("%s:%s:%s:%s", dep.Group, dep.Module, dep.Version, dep.Classifier)
	} else {
		dependencyId = fmt.Sprintf("%s:%s:%s", dep.Group, dep.Module, dep.Version)
	}

	if _, exists := allDeps[dependencyId]; exists {
		existingDep := allDeps[dependencyId]
		existingScopes := make(map[string]bool)
		for _, s := range existingDep.Scopes {
			existingScopes[s] = true
		}
		for _, s := range scopes {
			if !existingScopes[s] {
				existingDep.Scopes = append(existingDep.Scopes, s)
			}
		}
		allDeps[dependencyId] = existingDep
	} else {
		depType := "jar"
		if dep.Type != "" {
			depType = dep.Type
		}

		depInfo := flexpack.DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", dep.Group, dep.Module),
			Version: dep.Version,
			Type:    depType,
			Scopes:  scopes,
		}
		allDeps[dependencyId] = depInfo
	}

	if parent != "" {
		if gf.dependencyGraph[parent] == nil {
			gf.dependencyGraph[parent] = []string{}
		}
		gf.dependencyGraph[parent] = append(gf.dependencyGraph[parent], dependencyId)
	}
	for _, child := range dep.Children {
		gf.processGradleDependency(child, dependencyId, scopes, allDeps)
	}
}
