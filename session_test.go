package session

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestExtract(t *testing.T) {
	for _, tc := range []struct{ source, line, text, role string }{
		{Codex, `{"type":"response_item","payload":{"role":"user","content":[{"text":"<external_codex_apps_open_page>{}</external_codex_apps_open_page>"},{"text":"hello"}]}}`, "hello", "user"},
		{Codex, `{"payload":{"role":"assistant","phase":"commentary","content":[{"text":"answer"}]}}`, "answer", "assistant"},
		{Claude, `{"type":"user","message":{"role":"user","content":"<system-reminder>hidden</system-reminder> question"}}`, "question", "user"},
		{Claude, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","text":"hidden"},{"type":"text","text":"answer"}]}}`, "answer", "assistant"},
		{Codex, `{"role":"user","text":"broken"} garbage`, "", ""},
	} {
		got := Extract(tc.source, []byte(tc.line))
		if got.Text != tc.text || got.Role != tc.role {
			t.Errorf("%s: got %+v", tc.line, got)
		}
	}
	long := strings.Repeat("x", 3<<20)
	got := Extract(Codex, []byte(`{"role":"user","text":"`+long+`"}`))
	if got.Text != long {
		t.Fatal("long message changed")
	}
}

func TestDiscoverAndOpenCompressed(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "archived_sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const id = "019e992e-38ca-7a92-8dc6-45f54a2bad8d"
	path := filepath.Join(dir, "rollout-"+id+".jsonl.zst")
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	data := enc.EncodeAll([]byte("hello\n"), nil)
	_ = enc.Close()
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "no-id.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	files, found, err := Discover(Codex, home)
	if err != nil || !found || len(files) != 1 || files[0].ID != id || !files[0].Archived {
		t.Fatalf("files=%+v found=%v err=%v", files, found, err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil || string(b) != "hello\n" {
		t.Fatalf("data=%q err=%v", b, err)
	}
}
