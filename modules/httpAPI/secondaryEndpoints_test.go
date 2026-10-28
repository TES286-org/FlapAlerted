//go:build !disable_mod_httpAPI

package httpAPI

import "testing"

func TestPrometheusQuoteLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain hostname", input: "router-a.example", want: `"router-a.example"`},
		{name: "ipv4 address", input: "192.0.2.1", want: `"192.0.2.1"`},
		{name: "ipv6 address", input: "fdcf:8538:9ad5::1", want: `"fdcf:8538:9ad5::1"`},
		{name: "double quote escaped", input: `a"b`, want: `"a\"b"`},
		{name: "backslash escaped", input: `a\b`, want: `"a\\b"`},
		{name: "newline escaped", input: "a\nb", want: `"a\nb"`},
		{name: "empty string", input: "", want: `""`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := prometheusQuoteLabel(tc.input)
			if got != tc.want {
				t.Errorf("prometheusQuoteLabel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
