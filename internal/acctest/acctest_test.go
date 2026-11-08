package acctest

import "testing"

func TestIsTestResource(t *testing.T) {
	cases := []struct {
		name     string
		hostName string
		tags     []string
		want     bool
	}{
		{"host-name prefix", TestNamePrefix + "-123", nil, true},
		{"test tag", "my-host", []string{"web", TestTag}, true},
		{"neither prefix nor tag", "my-host", []string{"web"}, false},
		{"empty", "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTestResource(tc.hostName, tc.tags); got != tc.want {
				t.Errorf("IsTestResource(%q, %v) = %v, want %v", tc.hostName, tc.tags, got, tc.want)
			}
		})
	}
}
