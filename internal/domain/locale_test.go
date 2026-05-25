package domain

import "testing"

func TestResolveAssortmentLanguage(t *testing.T) {
	cases := []struct {
		name   string
		locale string
		want   string
	}{
		{name: "empty falls back to en", locale: "", want: "en"},
		{name: "whitespace only falls back to en", locale: "   ", want: "en"},
		{name: "bare language passes through", locale: "fi", want: "fi"},
		{name: "BCP-47 region stripped", locale: "en-FI", want: "en"},
		{name: "BCP-47 region stripped fi-FI", locale: "fi-FI", want: "fi"},
		{name: "multi-subtag keeps only primary language", locale: "zh-Hans-CN", want: "zh"},
		{name: "leading/trailing whitespace trimmed", locale: "  fr-FR  ", want: "fr"},
		{name: "whitespace inside primary tag trimmed", locale: "  de  -DE", want: "de"},
		{name: "empty primary subtag falls back to en", locale: "-FI", want: "en"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveAssortmentLanguage(tc.locale); got != tc.want {
				t.Errorf("ResolveAssortmentLanguage(%q) = %q, want %q", tc.locale, got, tc.want)
			}
		})
	}
}
