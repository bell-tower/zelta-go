package version

import "testing"

func TestString(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "9.9.9"
	if String() != "Zelta 9.9.9" || Zprune() != "zprune 9.9.9" {
		t.Errorf("banners = %q %q", String(), Zprune())
	}
}
