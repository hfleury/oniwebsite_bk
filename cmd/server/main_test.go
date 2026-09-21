package main

import "testing"

func TestIsPageRoute(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/index.html", true},
		{"/pt", true},
		{"/pt/", true},
		{"/pt/services/staff-augmentation", true},
		{"/sv", true},
		{"/sv/", true},
		{"/sv/services/staff-augmentation", true},
		{"/services/staff-augmentation", true},

		{"/en", false},
		{"/ptx", false},
		{"/pt.png", false},
		{"/svg/logo.svg", false},
		{"/assets/index-abc123.js", false},
		{"/api/translations", false},
		{"/services", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isPageRoute(tt.path); got != tt.want {
				t.Errorf("isPageRoute(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
