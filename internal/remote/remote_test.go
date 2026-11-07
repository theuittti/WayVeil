package remote

import "testing"

func TestShellQuote(t *testing.T) {
	cases := map[string]string{`O'Brien`: `'O'\''Brien'`, "": `''`, "a b": `'a b'`}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q)=%q want %q", in, got, want)
		}
	}
}

func TestWithEnv(t *testing.T) {
	got := string(WithEnv([]byte("echo hi\n"), map[string]string{"B": "2", "A": "x'y"}))
	want := "export A='x'\\''y'\nexport B='2'\necho hi\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExitErrorMessage(t *testing.T) {
	e := &ExitError{Code: 3, Stderr: "boom\n"}
	if e.Error() != "exit status 3: boom" {
		t.Fatalf("%q", e.Error())
	}
}
