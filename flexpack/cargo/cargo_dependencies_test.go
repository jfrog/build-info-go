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
