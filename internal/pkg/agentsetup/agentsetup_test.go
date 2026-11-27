package agentsetup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndRead(t *testing.T) {
	exe := []byte("MZ\x90\x00fake executable bytes")
	env := "AGENT_SERVER_URL=https://api.example.uz\nAGENT_TOKEN=pa_secret\n"

	out, err := Append(exe, env)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "agent.exe")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != env {
		t.Fatalf("Read = %q, want %q", got, env)
	}
	if base, _, ok := split(out); !ok || string(base) != string(exe) {
		t.Fatalf("executable bytes changed: ok=%v base=%q", ok, base)
	}
}

func TestAppendReplacesEarlierSettings(t *testing.T) {
	exe := []byte("MZ executable")
	first, _ := Append(exe, "AGENT_TOKEN=old\n")
	second, _ := Append(first, "AGENT_TOKEN=new\n")
	base, env, ok := split(second)
	if !ok || env != "AGENT_TOKEN=new\n" || string(base) != string(exe) {
		t.Fatalf("got ok=%v env=%q base=%q", ok, env, base)
	}
}

func TestReadPlainExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.exe")
	if err := os.WriteFile(path, []byte("MZ just a program, nothing embedded"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil || got != "" {
		t.Fatalf("Read = %q, %v; want empty", got, err)
	}
}

func TestTooLarge(t *testing.T) {
	if _, err := Append([]byte("MZ"), string(make([]byte, MaxEnvSize+1))); err == nil {
		t.Fatal("expected an error for oversized settings")
	}
}
