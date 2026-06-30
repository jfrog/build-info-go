package cargo

import "strings"

// parsePackageId normalizes a cargo metadata package id into (name, version, source).
// Handles both the pre-1.77 form "name version (source)" and the >=1.77
// PackageIdSpec form "source#name@version" or "source#version".
func parsePackageId(id string) (name, version, source string) {
	id = strings.TrimSpace(id)
	// New PackageIdSpec form: contains '#'.
	if hashIdx := strings.LastIndex(id, "#"); hashIdx != -1 {
		source = id[:hashIdx]
		spec := id[hashIdx+1:]
		if at := strings.LastIndex(spec, "@"); at != -1 {
			// "name@version"
			return spec[:at], spec[at+1:], source
		}
		// "version" only — derive name from the last path segment of source.
		version = spec
		name = lastPathSegment(source)
		return name, version, source
	}
	// Old form: "name version (source)".
	openParen := strings.Index(id, " (")
	if openParen != -1 && strings.HasSuffix(id, ")") {
		source = id[openParen+2 : len(id)-1]
		id = id[:openParen]
	}
	fields := strings.Fields(id)
	if len(fields) >= 2 {
		return fields[0], fields[1], source
	}
	return id, "", source
}

// lastPathSegment returns the final path/url segment, stripping any scheme prefix
// like "path+file:///a/b/mycrate" -> "mycrate".
func lastPathSegment(s string) string {
	s = strings.TrimRight(s, "/")
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		return s[idx+1:]
	}
	return s
}

// scopeForDepKinds maps cargo dep_kinds to a build-info scope and decides inclusion.
// Normal ("") -> "prod", "build" -> "build", "dev" -> "dev" (only if includeDev).
// A dependency with multiple kinds prefers normal > build > dev.
func scopeForDepKinds(kinds []CargoDepKind, includeDev bool) (string, bool) {
	hasNormal, hasBuild, hasDev := false, false, false
	for _, k := range kinds {
		switch k.Kind {
		case "":
			hasNormal = true
		case "build":
			hasBuild = true
		case "dev":
			hasDev = true
		}
	}
	switch {
	case hasNormal:
		return "prod", true
	case hasBuild:
		return "build", true
	case hasDev:
		return "dev", includeDev
	default:
		return "prod", true
	}
}

// buildRequestedBy reverses the resolve graph: dependency id -> parent ids.
func buildRequestedBy(meta *CargoMetadata) map[string][]string {
	rb := make(map[string][]string)
	for _, node := range meta.Resolve.Nodes {
		for _, childId := range node.Dependencies {
			rb[childId] = appendUnique(rb[childId], node.Id)
		}
	}
	return rb
}

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
