package remote

import (
	"strings"
	"testing"
)

func TestValidateConnection(t *testing.T) {
	others := []Connection{{Name: "build-box", Destination: "me@build"}}
	cases := []struct {
		name string
		next Connection
		want string
	}{
		{"valid", Connection{Name: "gpu box", Destination: "me@gpu.example.com"}, ""},
		{"alias", Connection{Name: "gpu", Destination: "gpu"}, ""},
		{"port style and underscore", Connection{Name: "gpu", Destination: "_me@10.0.0.1:22"}, ""},
		{"unicode name", Connection{Name: "café ☕", Destination: "gpu"}, ""},
		{"forty bytes", Connection{Name: strings.Repeat("a", 40), Destination: "gpu"}, ""},
		{"other case is distinct", Connection{Name: "Build-Box", Destination: "gpu"}, ""},
		{"empty name", Connection{Destination: "gpu"}, "a connection needs a name"},
		{"leading space", Connection{Name: " gpu", Destination: "gpu"}, "a connection name cannot start or end with a space"},
		{"trailing tab", Connection{Name: "gpu\t", Destination: "gpu"}, "a connection name cannot start or end with a space"},
		{"ref separator", Connection{Name: "a::b", Destination: "gpu"}, `a connection name cannot contain "::"`},
		{"slash", Connection{Name: "a/b", Destination: "gpu"}, "a connection name cannot contain /"},
		{"control character", Connection{Name: "a\x1bb", Destination: "gpu"}, "a connection name cannot contain control characters"},
		{"invalid utf-8", Connection{Name: "a\xffb", Destination: "gpu"}, "a connection name cannot contain control characters"},
		{"forty-one bytes", Connection{Name: strings.Repeat("a", 41), Destination: "gpu"}, "a connection name can be at most 40 bytes long"},
		{"duplicate", Connection{Name: "build-box", Destination: "gpu"}, "a connection named build-box already exists"},
		{"no destination", Connection{Name: "gpu"}, "a connection needs an SSH destination, such as user@host or an alias from ~/.ssh/config"},
		{"option destination", Connection{Name: "gpu", Destination: "-oProxyCommand=x"}, "an SSH destination holds only letters, digits and _ . @ : -, and starts with a letter, digit or _"},
		{"dot first", Connection{Name: "gpu", Destination: ".gpu"}, "an SSH destination holds only letters, digits and _ . @ : -, and starts with a letter, digit or _"},
		{"space", Connection{Name: "gpu", Destination: "me@gpu box"}, "an SSH destination holds only letters, digits and _ . @ : -, and starts with a letter, digit or _"},
		{"url", Connection{Name: "gpu", Destination: "ssh://gpu"}, "an SSH destination holds only letters, digits and _ . @ : -, and starts with a letter, digit or _"},
		{"long destination", Connection{Name: "gpu", Destination: strings.Repeat("a", 256)}, "an SSH destination can be at most 255 bytes long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConnection(tc.next, others)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("ValidateConnection(%q, %q) = %q, want %q", tc.next.Name, tc.next.Destination, got, tc.want)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		in   string
		want Ref
		ok   bool
	}{
		{"build-box::a1b2c3d4", Ref{Host: "build-box", ID: "a1b2c3d4"}, true},
		{"gpu box::a1", Ref{Host: "gpu box", ID: "a1"}, true},
		{"a::b::c", Ref{Host: "a", ID: "b::c"}, true},
		{"a1b2c3d4", Ref{}, false},
		{"::a1", Ref{}, false},
		{"build-box::", Ref{}, false},
		{"", Ref{}, false},
	}
	for _, tc := range cases {
		got, ok := ParseRef(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseRef(%q) = %+v, %v, want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
		if ok && got.String() != tc.in {
			t.Errorf("ParseRef(%q).String() = %q", tc.in, got.String())
		}
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"a1b2c3d4", "A_b-9"} {
		if err := validID(id); err != nil {
			t.Errorf("validID(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"", "a b", "a;b", "a::b", "$(x)", "a\n"} {
		if err := validID(id); err == nil {
			t.Errorf("validID(%q) accepted it", id)
		}
	}
}
