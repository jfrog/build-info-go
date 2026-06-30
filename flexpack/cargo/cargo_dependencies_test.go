package cargo

import "testing"

func TestParsePackageId(t *testing.T) {
	cases := []struct {
		id, wantName, wantVer, wantSrc string
	}{
		// pre-1.77 opaque form
		{"serde 1.0.197 (registry+https://github.com/rust-lang/crates.io-index)", "serde", "1.0.197", "registry+https://github.com/rust-lang/crates.io-index"},
		// >=1.77 PackageIdSpec form
		{"registry+https://github.com/rust-lang/crates.io-index#serde@1.0.197", "serde", "1.0.197", "registry+https://github.com/rust-lang/crates.io-index"},
		// PackageIdSpec without explicit name (name is last path segment / before @)
		{"path+file:///work/mycrate#0.1.0", "mycrate", "0.1.0", "path+file:///work/mycrate"},
		// PackageIdSpec local with name@version
		{"path+file:///work#mycrate@0.1.0", "mycrate", "0.1.0", "path+file:///work"},
	}
	for _, c := range cases {
		name, ver, src := parsePackageId(c.id)
		if name != c.wantName || ver != c.wantVer || src != c.wantSrc {
			t.Errorf("parsePackageId(%q) = (%q,%q,%q), want (%q,%q,%q)",
				c.id, name, ver, src, c.wantName, c.wantVer, c.wantSrc)
		}
	}
}

func TestScopeForDepKinds(t *testing.T) {
	prod, inc := scopeForDepKinds([]CargoDepKind{{Kind: ""}}, false)
	if !inc || prod != "prod" {
		t.Errorf("normal dep: got (%q,%v), want (prod,true)", prod, inc)
	}
	build, inc := scopeForDepKinds([]CargoDepKind{{Kind: "build"}}, false)
	if !inc || build != "build" {
		t.Errorf("build dep: got (%q,%v), want (build,true)", build, inc)
	}
	// dev dep excluded when includeDev=false
	if _, inc := scopeForDepKinds([]CargoDepKind{{Kind: "dev"}}, false); inc {
		t.Error("dev dep should be excluded when includeDev=false")
	}
	dev, inc := scopeForDepKinds([]CargoDepKind{{Kind: "dev"}}, true)
	if !inc || dev != "dev" {
		t.Errorf("dev dep includeDev: got (%q,%v), want (dev,true)", dev, inc)
	}
}

func TestBuildRequestedBy(t *testing.T) {
	meta := &CargoMetadata{
		Resolve: CargoResolve{
			Root: "root 0.1.0 (path+file:///r)",
			Nodes: []CargoNode{
				{Id: "root 0.1.0 (path+file:///r)", Dependencies: []string{"a 1.0.0 (registry+x)"}},
				{Id: "a 1.0.0 (registry+x)", Dependencies: []string{"b 2.0.0 (registry+x)"}},
				{Id: "b 2.0.0 (registry+x)"},
			},
		},
	}
	rb := buildRequestedBy(meta)
	if len(rb["a 1.0.0 (registry+x)"]) != 1 || rb["a 1.0.0 (registry+x)"][0] != "root 0.1.0 (path+file:///r)" {
		t.Errorf("a should be requested by root, got %v", rb["a 1.0.0 (registry+x)"])
	}
	if len(rb["b 2.0.0 (registry+x)"]) != 1 || rb["b 2.0.0 (registry+x)"][0] != "a 1.0.0 (registry+x)" {
		t.Errorf("b should be requested by a, got %v", rb["b 2.0.0 (registry+x)"])
	}
}
