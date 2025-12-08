package flexpack

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jfrog/build-info-go/flexpack"
	"github.com/jfrog/gofrog/log"
)

var (
	groupRegex       = regexp.MustCompile(`group\s*[=:]\s*['"]([^'"]+)['"]`)
	nameRegex        = regexp.MustCompile(`(?:rootProject\.)?name\s*[=:]\s*['"]([^'"]+)['"]`)
	versionRegex     = regexp.MustCompile(`version\s*[=:]\s*['"]([^'"]+)['"]`)
	rootProjectRegex = regexp.MustCompile(`rootProject\.name\s*[=:]\s*['"]([^'"]+)['"]`)
	includeRegex     = regexp.MustCompile(`['"]([^'"]+)['"]`)
	depRegex         = regexp.MustCompile(`(implementation|compileOnly|runtimeOnly|testImplementation|testCompileOnly|testRuntimeOnly|api|compile|runtime|annotationProcessor|kapt|ksp)\s*[\(\s]['"]([^'"]+)['"]`)
	depMapRegex      = regexp.MustCompile(`(implementation|compileOnly|runtimeOnly|testImplementation|testCompileOnly|testRuntimeOnly|api|compile|runtime|annotationProcessor|kapt|ksp)\s*(?:\(|\s)\s*group\s*[:=]\s*['"]([^'"]+)['"]\s*,\s*name\s*[:=]\s*['"]([^'"]+)['"](?:,\s*version\s*[:=]\s*['"]([^'\"]+)['\"])?`)
	depProjectRegex  = regexp.MustCompile(`(implementation|compileOnly|runtimeOnly|testImplementation|testCompileOnly|testRuntimeOnly|api|compile|runtime|annotationProcessor|kapt|ksp)\s*(?:\(|\s)\s*project\s*(?:(?:(?:\(\s*path\s*:\s*))|(?:\(?\s*))['"]([^'"]+)['"]`)
)

func (gf *GradleFlexPack) parseBuildGradleMetadata(content string) (groupId, artifactId, version string) {
	groupMatch := groupRegex.FindStringSubmatch(content)
	if len(groupMatch) > 1 {
		groupId = groupMatch[1]
	} else {
		groupId = "unspecified"
	}

	// Extract name (artifactId) - can be rootProject.name or just name
	nameMatch := nameRegex.FindStringSubmatch(content)
	if len(nameMatch) > 1 {
		artifactId = nameMatch[1]
	}

	// Extract version
	versionMatch := versionRegex.FindStringSubmatch(content)
	if len(versionMatch) > 1 {
		version = versionMatch[1]
	} else {
		version = "unspecified"
	}
	return
}

func (gf *GradleFlexPack) parseFromBuildGradle(moduleName string) {
	contentBytes, _, err := gf.getBuildFileContent(moduleName)
	if err != nil {
		if moduleName == "" && gf.buildGradlePath == "" {
			return
		}
		log.Warn("Failed to read build.gradle for dependency parsing: " + err.Error())
		return
	}

	content := string(contentBytes)
	depsContent := gf.extractDependenciesBlock(content)
	if depsContent == "" {
		log.Debug("No dependencies block found in build.gradle")
		return
	}
	allDeps := make(map[string]flexpack.DependencyInfo)

	// 1. String notation: "group:artifact:version"
	matches := depRegex.FindAllStringSubmatch(depsContent, -1)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		configType := match[1]
		depString := match[2]

		// Parse dependency string (group:artifact:version[:classifier])
		parts := strings.Split(depString, ":")
		if len(parts) < 3 {
			continue
		}

		groupId := parts[0]
		artifactId := parts[1]
		version := parts[2]
		classifier := ""
		if len(parts) >= 4 {
			classifier = parts[3]
		}

		gf.addDependency(configType, groupId, artifactId, version, classifier, allDeps)
	}

	// 2. Map notation: group: '...', name: '...', version: '...'
	mapMatches := depMapRegex.FindAllStringSubmatch(depsContent, -1)
	for _, match := range mapMatches {
		if len(match) < 4 {
			continue
		}
		configType := match[1]
		groupId := match[2]
		artifactId := match[3]
		version := ""
		if len(match) >= 5 {
			version = match[4]
		}
		gf.addDependency(configType, groupId, artifactId, version, "", allDeps)
	}

	// 3. Project notation: project(':path')
	projMatches := depProjectRegex.FindAllStringSubmatch(depsContent, -1)
	for _, match := range projMatches {
		if len(match) < 3 {
			continue
		}
		configType := match[1]
		projectPath := match[2]
		projectPath = strings.TrimPrefix(projectPath, ":")

		parts := strings.Split(projectPath, ":")
		artifactId := parts[len(parts)-1]
		groupId := gf.groupId
		version := gf.projectVersion

		if meta, ok := gf.modulesMap[projectPath]; ok {
			groupId = meta.Group
			version = meta.Version
			artifactId = meta.Artifact
		}
		gf.addDependency(configType, groupId, artifactId, version, "", allDeps)
	}
	for _, dep := range allDeps {
		gf.dependencies = append(gf.dependencies, dep)
	}
}

func (gf *GradleFlexPack) extractDependenciesBlock(content string) string {
	// Find "dependencies {" or "dependencies{" pattern
	idx := strings.Index(content, "dependencies")
	if idx == -1 {
		return ""
	}
	remaining := content[idx+len("dependencies"):]
	braceIdx := strings.Index(remaining, "{")
	if braceIdx == -1 {
		return ""
	}

	start := idx + len("dependencies") + braceIdx + 1
	if start >= len(content) {
		return ""
	}

	braceCount := 1
	end := start
	inLineComment := false
	inBlockComment := false
	inString := false
	stringChar := byte(0)

	for i := start; i < len(content) && braceCount > 0; i++ {
		char := content[i]

		if inLineComment {
			if char == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if char == '*' && i+1 < len(content) && content[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			if char == '\\' {
				i++
				continue
			}
			if char == stringChar {
				inString = false
			}
			continue
		}

		if char == '/' {
			if i+1 < len(content) {
				if content[i+1] == '/' {
					inLineComment = true
					i++
					continue
				} else if content[i+1] == '*' {
					inBlockComment = true
					i++
					continue
				}
			}
		}

		if char == '"' || char == '\'' {
			inString = true
			stringChar = char
			continue
		}

		switch char {
		case '{':
			braceCount++
		case '}':
			braceCount--
		}
		end = i
	}

	if braceCount != 0 {
		log.Debug("Unbalanced braces in dependencies block")
		return ""
	}
	return content[start:end]
}

func (gf *GradleFlexPack) addDependency(configType, groupId, artifactId, version, classifier string, allDeps map[string]flexpack.DependencyInfo) {
	var dependencyId string
	if classifier != "" {
		dependencyId = fmt.Sprintf("%s:%s:%s:%s", groupId, artifactId, version, classifier)
	} else {
		dependencyId = fmt.Sprintf("%s:%s:%s", groupId, artifactId, version)
	}
	scopes := gf.mapGradleConfigurationToScopes(configType)

	if existing, ok := allDeps[dependencyId]; ok {
		existingScopes := make(map[string]bool)
		for _, s := range existing.Scopes {
			existingScopes[s] = true
		}
		for _, s := range scopes {
			if !existingScopes[s] {
				existing.Scopes = append(existing.Scopes, s)
			}
		}
		allDeps[dependencyId] = existing
	} else {
		depInfo := flexpack.DependencyInfo{
			ID:      dependencyId,
			Name:    fmt.Sprintf("%s:%s", groupId, artifactId),
			Version: version,
			Type:    "jar",
			Scopes:  scopes,
		}
		allDeps[dependencyId] = depInfo
	}
}

// stripComments removes single-line (//) and multi-line (/* */) comments from content
func (gf *GradleFlexPack) stripComments(content string) string {
	var result strings.Builder
	inBlockComment := false
	inString := false
	stringChar := byte(0)
	i := 0

	for i < len(content) {
		char := content[i]

		// Handle string literals to avoid removing comments inside strings
		if !inBlockComment {
			if inString {
				result.WriteByte(char)
				if char == '\\' && i+1 < len(content) {
					i++
					result.WriteByte(content[i])
				} else if char == stringChar {
					inString = false
				}
				i++
				continue
			}
			if char == '"' || char == '\'' {
				inString = true
				stringChar = char
				result.WriteByte(char)
				i++
				continue
			}
		}

		// Handle block comment start
		if !inBlockComment && i+1 < len(content) && char == '/' && content[i+1] == '*' {
			inBlockComment = true
			i += 2
			continue
		}

		// Handle block comment end
		if inBlockComment && i+1 < len(content) && char == '*' && content[i+1] == '/' {
			inBlockComment = false
			i += 2
			continue
		}

		// Skip content inside block comment
		if inBlockComment {
			// Preserve newlines for line counting
			if char == '\n' {
				result.WriteByte('\n')
			}
			i++
			continue
		}

		// Handle single-line comment
		if i+1 < len(content) && char == '/' && content[i+1] == '/' {
			// Skip until end of line
			for i < len(content) && content[i] != '\n' {
				i++
			}
			continue
		}
		result.WriteByte(char)
		i++
	}
	return result.String()
}

// parseSettingsGradleModules parses the settings.gradle content to extract modules
func (gf *GradleFlexPack) parseSettingsGradleModules(content string) []string {
	strippedContent := gf.stripComments(content)

	var modules []string
	// Root module
	modules = append(modules, "")
	lines := strings.Split(strippedContent, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "include") && !strings.HasPrefix(trimmed, "includeBuild") {
			matches := includeRegex.FindAllStringSubmatch(trimmed, -1)
			for _, match := range matches {
				if len(match) > 1 {
					moduleName := strings.TrimPrefix(match[1], ":")
					if moduleName != "" {
						modules = append(modules, moduleName)
					}
				}
			}
		}
	}
	return modules
}
