package remote

import "testing"

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
