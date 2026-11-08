package gui

import "testing"

// The webview may only navigate inside the app's own origin.
func TestAppURL(t *testing.T) {
	for s, want := range map[string]bool{
		"wails://localhost/":                true,
		"wails://localhost/index.html#/vms": true,
		"wails://localhost:9245/":           false,
		"wails://user@localhost/":           false,
		"wails://evil.example/":             false,
		"https://localhost/":                false,
		"http://wails.localhost/":           false,
		"file:///etc/passwd":                false,
		"javascript:alert(1)":               false,
		"":                                  false,
	} {
		if got := appURL(s); got != want {
			t.Errorf("appURL(%q) = %v, want %v", s, got, want)
		}
	}
}
