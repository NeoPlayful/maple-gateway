package router

import "testing"

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"example.com", "example.com"},
		{"EXAMPLE.com", "example.com"},
		{"example.com:8080", "example.com"},
		{"  example.com  ", "example.com"},
		{"example.com.", "example.com"},
		{"shop-a.test", "shop-a.test"},
		{"xn--fsqu00a.xn--0zwm56d", "xn--fsqu00a.xn--0zwm56d"}, // 已是 punycode 不变
		{"中文.example", "xn--fiq228c.example"},                  // IDN → punycode
		{"[::1]:8080", "::1"},                                  // IPv6 字面量剥端口留地址
	}
	for _, c := range cases {
		got, err := NormalizeHost(c.in)
		if err != nil {
			t.Errorf("NormalizeHost(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeHost_Invalid(t *testing.T) {
	invalid := []string{"", "   ", "http://example.com", "exa mple.com", "a..b", "\\example"}
	for _, in := range invalid {
		if _, err := NormalizeHost(in); err == nil {
			t.Errorf("NormalizeHost(%q) should error", in)
		}
	}
}
