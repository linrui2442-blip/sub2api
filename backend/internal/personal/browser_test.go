package personal

import "testing"

func TestLocalHTTPURL(t *testing.T) {
	cases := []struct {
		addr string
		path string
		want string
	}{
		{addr: "127.0.0.1:8080", path: "/setup", want: "http://127.0.0.1:8080/setup"},
		{addr: "0.0.0.0:8080", path: "login", want: "http://127.0.0.1:8080/login"},
		{addr: "[::]:8080", path: "/login", want: "http://[::1]:8080/login"},
	}
	for _, tc := range cases {
		if got := localHTTPURL(tc.addr, tc.path); got != tc.want {
			t.Fatalf("localHTTPURL(%q, %q) = %q, want %q", tc.addr, tc.path, got, tc.want)
		}
	}
}

func TestLocalHTTPURLRejectsInvalidAddress(t *testing.T) {
	if got := localHTTPURL("not-an-address", "/"); got != "" {
		t.Fatalf("invalid address produced URL %q", got)
	}
}
