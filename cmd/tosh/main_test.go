package main

import "testing"

func TestBootstrapOptionsRespectsPositionalsAndFlagValues(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		lang, path string
	}{
		{[]string{"--lang", "en", "--config", "/tmp/c", "-h"}, "en", "/tmp/c"},
		{[]string{"--config=/tmp/c", "--lang=zh", "-h"}, "zh", "/tmp/c"},
		{[]string{"--config", "--lang", "prompt"}, "", "--lang"},
		{[]string{"prompt", "--lang", "en"}, "", ""},
		{[]string{"--", "--lang", "en"}, "", ""},
	} {
		l, p := bootstrapOptions(tc.args)
		if l != tc.lang || p != tc.path {
			t.Fatalf("%q: %s %s", tc.args, l, p)
		}
	}
}
