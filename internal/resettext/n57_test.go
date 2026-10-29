package resettext

import "testing"

func TestN57IndependentDayResetClause(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"Day 42/ A calendar add-on. Just kidding, today is a reset. Enjoy!", "signal"},
		{"Day 42/ A new calendar plugin. Just kidding, today is a reset. Enjoy!", "signal"},
		{"Day 42/ Today is a reset. Enjoy!", "signal"},
		{"Day 23/ Today is a reset. Just kidding.", ""},
		{"Day 23/ Just kidding, today is a reset.", ""},
		{"Day 23/ A reset plugin. Just kidding, today is a reset.", ""},
		{"Day 23/ Plugins. Just kidding, today is a reset?", ""},
		{"Day 23/ Plugins. Just kidding, today is not a reset.", ""},
		{"Day 23/ Plugins. Just kidding, today is a reset. This is a joke.", ""},
		{"Day 23/ Plugins. Just kidding, today is a reset. Only for some users.", ""},
	} {
		t.Run(tc.text, func(t *testing.T) {
			if got := Classify(tc.text); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
