package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"validate"}, &out, &errOut); code != 0 {
		t.Fatalf("validate exit %d: %s", code, errOut.String())
	}
	for _, id := range []string{"landing", "expenses", "booking", "admin"} {
		if !strings.Contains(out.String(), "ok  "+id) {
			t.Errorf("validate output missing %s:\n%s", id, out.String())
		}
	}

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("id: Bad Id\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"validate", bad}, &out, &errOut); code != 1 {
		t.Errorf("malformed scenario: exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "bad.yaml") {
		t.Errorf("error output does not name the file: %s", errOut.String())
	}
}

func TestFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--list"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "booking") {
		t.Errorf("--list: exit %d, %s", code, out.String())
	}
	cases := [][]string{
		{"--difficulty", "brutal"},
		{"--seed", "abc", "--scenario", "landing"},
		{"--seed", "5"},
		{"--scenario", "nope"},
		{"stray"},
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, args := range cases {
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if code := run([]string{"--reset"}, &out, &errOut); code != 0 {
		t.Errorf("--reset: exit %d", code)
	}
}
