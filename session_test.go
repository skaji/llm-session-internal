package session

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestCodexAppsContext(t *testing.T) {
	const page = `<external_codex_apps_selected_page_part_1_of_1>snapshot</external_codex_apps_selected_page_part_1_of_1>`
	const reply = `<send_user_message_question_reply>[{"question":"Choose","answer":"2"}]</send_user_message_question_reply>`
	for _, tc := range []struct {
		name  string
		texts []string
		kinds []string
		want  string
	}{
		{"selected page", []string{page}, []string{"additional_content.codex_apps_selected_page_part_1_of_1"}, ""},
		{"future part", []string{"context", reply}, []string{"additional_content.codex_apps_selected_page_part_2_of_3", "user.text"}, reply},
		{"other additional content", []string{"keep"}, []string{"additional_content.other"}, "keep"},
		{"user quoted page", []string{page}, []string{"user.text"}, page},
		{"legacy selected page", []string{page, reply}, nil, reply},
		{"legacy mismatched tags", []string{`<external_codex_apps_page>text</external_codex_apps_other>`}, nil, `<external_codex_apps_page>text</external_codex_apps_other>`},
		{"legacy discussion", []string{"Please exclude " + page}, nil, "Please exclude " + page},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := make([]map[string]string, 0, len(tc.texts))
			for _, text := range tc.texts {
				content = append(content, map[string]string{"type": "input_text", "text": text})
			}
			payload := map[string]any{"role": "user", "content": content}
			if tc.kinds != nil {
				payload["internal_chat_message_metadata_passthrough"] = map[string]any{"content_item_kinds": tc.kinds}
			}
			line, err := json.Marshal(map[string]any{"payload": payload})
			if err != nil {
				t.Fatal(err)
			}
			if got := Extract(Codex, line).Text; got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

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

func TestDiscoverClaudeCompressed(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "projects", "example")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const id = "019e992e-38ca-7a92-8dc6-45f54a2bad8d"
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = encoder.Close() }()
	content := []byte(`{"type":"user","message":{"role":"user","content":"hello"}}` + "\n")
	for _, suffix := range []string{".jsonl.zst", ".JSONL.ZST"} {
		path := filepath.Join(dir, id+suffix)
		if err := os.WriteFile(path, encoder.EncodeAll(content, nil), 0o600); err != nil {
			t.Fatal(err)
		}
		files, found, err := Discover(Claude, home)
		if err != nil || !found || len(files) != 1 || files[0].ID != id || files[0].Path != path {
			t.Fatalf("files=%+v found=%v err=%v", files, found, err)
		}
		reader, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		record := Extract(Claude, data)
		if record.Role != "user" || record.Text != "hello" {
			t.Fatalf("record=%+v", record)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}
