package netutil

import "testing"

func TestIsLoopbackHostCoversLoopbackRange(t *testing.T) {
	for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", "127.0.0.2", "127.42.1.9", "::1"} {
		if !IsLoopbackHost(host) {
			t.Fatalf("IsLoopbackHost(%q) = false", host)
		}
	}
	for _, host := range []string{"example.com", "10.0.0.1", "::2"} {
		if IsLoopbackHost(host) {
			t.Fatalf("IsLoopbackHost(%q) = true", host)
		}
	}
}
