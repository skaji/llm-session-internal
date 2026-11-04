// Package session discovers and decodes local Codex and Claude conversations.
package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	Codex  = "codex"
	Claude = "claude"
)

type (
	sessionFile struct {
		source, path, id, title string
		archived, titleKnown    bool
		info                    fs.FileInfo
	}
	File struct {
		Source, Path, ID, Title string
		Archived, TitleKnown    bool
		Info                    fs.FileInfo
	}
	Record struct {
		Text, Role, Phase, CWD, Title string
		Timestamps                    []time.Time
		ForceTitle                    bool
	}
)

var uuidPattern = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func IDFromPath(path string) string {
	return strings.ToLower(uuidPattern.FindString(filepath.Base(path)))
}
func sessionIDFromPath(path string) string { return IDFromPath(path) }
func ValidID(id string) bool               { return len(id) == 36 && uuidPattern.FindString(id) == id }

func Discover(source, home string) ([]File, bool, error) {
	var files []sessionFile
	var found bool
	var err error
	switch source {
	case Codex:
		files, found, err = discoverCodexSessionFiles(home)
	case Claude:
		files, found, err = discoverClaudeSessionFiles(home)
	default:
		return nil, false, fmt.Errorf("unsupported session source %q", source)
	}
	out := make([]File, 0, len(files))
	for _, f := range files {
		if f.id != "" {
			out = append(out, File{f.source, f.path, f.id, f.title, f.archived, f.titleKnown, f.info})
		}
	}
	return out, found, err
}

func Extract(source string, line []byte) Record {
	e := extractSessionLine(source, line)
	return Record{e.text, e.role, e.phase, e.cwd, e.title, e.timestamps, e.forceTitle}
}
func Titles(path string) map[string]string { return loadCodexSessionTitles(path) }

// Decode reads plain or zstd-compressed JSONL from r.
func Decode(r io.Reader, path string) (io.ReadCloser, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".zst") {
		return io.NopCloser(r), nil
	}
	decoder, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	return decoder.IOReadCloser(), nil
}

// Open opens a plain or zstd-compressed session. The caller closes the reader.
func Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r, err := Decode(f, path)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &sessionReader{r, f}, nil
}

type sessionReader struct {
	io.ReadCloser
	f *os.File
}

func (r *sessionReader) Close() error {
	err := r.ReadCloser.Close()
	fileErr := r.f.Close()
	if err != nil {
		return err
	}
	return fileErr
}

// CodexTitle reads the latest title for a rollout in its own home.
func CodexTitle(path, id string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	var home string
	for dir := filepath.Dir(absolute); ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "sessions" || filepath.Base(dir) == "archived_sessions" {
			home = filepath.Dir(dir)
			break
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
	f, err := os.Open(filepath.Join(home, "session_index.jsonl"))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	var title string
	for {
		line, e := r.ReadBytes('\n')
		var entry struct {
			ID    string `json:"id"`
			Title string `json:"thread_name"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.ID == id {
			title = strings.TrimSpace(entry.Title)
		}
		if e != nil {
			return title
		}
	}
}
