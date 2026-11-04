# llm-session-internal

Shared Go library for discovering and reading local Codex and Claude Code
sessions. It discovers UUID-named sessions, reads JSONL and zstd-compressed JSONL for both sources, and extracts conversation text and metadata without truncating messages.
Consumers decide how to store, display, and limit the extracted content.
