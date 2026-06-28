package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

const promptHistoryPageLimit = 50

type PromptHistoryEntry struct {
	Text        string `json:"text"`
	At          int64  `json:"at"`
	SessionPath string `json:"sessionPath"`
	Turn        int    `json:"turn"`
}

type PromptHistoryResult struct {
	Entries     []PromptHistoryEntry `json:"entries"`
	Nonce       string               `json:"nonce"`
	OlderCursor string               `json:"olderCursor,omitempty"`
	HasOlder    bool                 `json:"hasOlder,omitempty"`
}

type promptHistoryRequest struct {
	Cursor string `json:"cursor"`
	Nonce  string `json:"nonce"`
}

type promptHistoryTape struct {
	nonce       string
	sessionDir  string
	sessionPath string
	entries     []PromptHistoryEntry
}

func (a *App) invalidatePromptHistoryCache() {
	a.promptHistoryMu.Lock()
	a.promptHistoryTape = nil
	a.promptHistoryMu.Unlock()
}

func (a *App) ScanPromptHistory(raw string) (PromptHistoryResult, error) {
	req := parsePromptHistoryRequest(raw)
	sessionDir := a.activeSessionDir()
	sessionPath := ""
	if ctrl := a.activeCtrl(); ctrl != nil {
		sessionPath = ctrl.SessionPath()
	}

	a.promptHistoryMu.Lock()
	defer a.promptHistoryMu.Unlock()

	if a.promptHistoryTape != nil &&
		a.promptHistoryTape.sessionDir == sessionDir &&
		a.promptHistoryTape.sessionPath == sessionPath {
		if req.Cursor == "" && req.Nonce == "" && strings.TrimSpace(raw) == a.promptHistoryTape.nonce {
			return PromptHistoryResult{Entries: nil, Nonce: a.promptHistoryTape.nonce}, nil
		}
	}

	if a.promptHistoryTape == nil ||
		a.promptHistoryTape.sessionDir != sessionDir ||
		a.promptHistoryTape.sessionPath != sessionPath {
		entries, err := a.promptHistoryEntries(sessionDir, sessionPath)
		if err != nil {
			return PromptHistoryResult{}, err
		}
		a.promptHistoryTape = &promptHistoryTape{
			nonce:       newPromptHistoryNonce(),
			sessionDir:  sessionDir,
			sessionPath: sessionPath,
			entries:     entries,
		}
	}

	offset := parsePromptHistoryCursor(req.Cursor)
	if offset > len(a.promptHistoryTape.entries) {
		offset = len(a.promptHistoryTape.entries)
	}
	end := offset + promptHistoryPageLimit
	if end > len(a.promptHistoryTape.entries) {
		end = len(a.promptHistoryTape.entries)
	}
	page := append([]PromptHistoryEntry(nil), a.promptHistoryTape.entries[offset:end]...)
	res := PromptHistoryResult{Entries: page, Nonce: a.promptHistoryTape.nonce}
	if end < len(a.promptHistoryTape.entries) {
		res.HasOlder = true
		res.OlderCursor = strconv.Itoa(end)
	}
	return res, nil
}

func parsePromptHistoryRequest(raw string) promptHistoryRequest {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return promptHistoryRequest{}
	}
	var req promptHistoryRequest
	if strings.HasPrefix(raw, "{") && json.Unmarshal([]byte(raw), &req) == nil {
		return req
	}
	return promptHistoryRequest{Nonce: raw}
}

func parsePromptHistoryCursor(cursor string) int {
	n, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func newPromptHistoryNonce() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (a *App) promptHistoryEntries(sessionDir, currentSessionPath string) ([]PromptHistoryEntry, error) {
	var out []PromptHistoryEntry
	currentSessionPath = filepath.Clean(currentSessionPath)
	if currentSessionPath != "." && currentSessionPath != "" {
		if info, err := os.Stat(currentSessionPath); err == nil && !info.IsDir() {
			entries, err := collectPromptHistoryEntries(currentSessionPath, info, sessionDisplayResolver(sessionDir, currentSessionPath))
			if err != nil {
				return nil, err
			}
			reversePromptHistory(entries)
			out = append(out, entries...)
		}
	}

	infos, err := agent.ListSessions(sessionDir)
	if err != nil {
		return out, nil
	}
	for _, info := range infos {
		if currentSessionPath != "" && samePath(info.Path, currentSessionPath) {
			continue
		}
		fileInfo, err := os.Stat(info.Path)
		if err != nil || fileInfo.IsDir() {
			continue
		}
		entries, err := collectPromptHistoryEntries(info.Path, fileInfo, sessionDisplayResolver(sessionDir, info.Path))
		if err != nil {
			return nil, err
		}
		reversePromptHistory(entries)
		out = append(out, entries...)
	}
	return out, nil
}

func (a *App) scanPromptHistoryFromDir(dir string) ([]PromptHistoryEntry, error) {
	infos, err := agent.ListSessions(dir)
	if err != nil {
		return []PromptHistoryEntry{}, nil
	}
	var out []PromptHistoryEntry
	for _, info := range infos {
		fileInfo, err := os.Stat(info.Path)
		if err != nil || fileInfo.IsDir() {
			continue
		}
		entries, err := collectPromptHistoryEntries(info.Path, fileInfo, sessionDisplayResolver(dir, info.Path))
		if err != nil {
			return nil, err
		}
		reversePromptHistory(entries)
		out = append(out, entries...)
	}
	return out, nil
}

func reversePromptHistory(entries []PromptHistoryEntry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}

func samePath(a, b string) bool {
	aa, aerr := filepath.Abs(a)
	bb, berr := filepath.Abs(b)
	if aerr == nil && berr == nil {
		return aa == bb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

type promptHistoryRecord struct {
	Kind           string          `json:"kind"`
	Type           string          `json:"type"`
	Role           string          `json:"role"`
	Text           string          `json:"text"`
	Content        string          `json:"content"`
	Time           json.RawMessage `json:"time"`
	Timestamp      json.RawMessage `json:"timestamp"`
	CreatedAt      json.RawMessage `json:"createdAt"`
	CreatedAtSnake json.RawMessage `json:"created_at"`
}

func collectPromptHistoryEntries(path string, info os.FileInfo, display func(string) string) ([]PromptHistoryEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if display == nil {
		display = func(s string) string { return s }
	}

	var out []PromptHistoryEntry
	turn := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec promptHistoryRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		text, ok := promptHistoryText(rec)
		if !ok {
			continue
		}
		text = strings.TrimSpace(display(text))
		if text == "" || control.IsSyntheticUserMessage(text) {
			turn++
			continue
		}
		out = append(out, PromptHistoryEntry{
			Text:        text,
			At:          promptHistoryTime(rec, info),
			SessionPath: path,
			Turn:        turn,
		})
		turn++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func promptHistoryText(rec promptHistoryRecord) (string, bool) {
	if rec.Kind == "user.message" || rec.Type == "user.message" {
		return rec.Text, strings.TrimSpace(rec.Text) != ""
	}
	if rec.Role == string(provider.RoleUser) || rec.Role == "user" {
		return rec.Content, strings.TrimSpace(rec.Content) != ""
	}
	return "", false
}

func promptHistoryTime(rec promptHistoryRecord, info os.FileInfo) int64 {
	for _, raw := range []json.RawMessage{rec.Time, rec.Timestamp, rec.CreatedAt, rec.CreatedAtSnake} {
		if at, ok := parsePromptHistoryTime(raw); ok {
			return at
		}
	}
	if info != nil {
		return info.ModTime().UnixMilli()
	}
	return 0
}

func parsePromptHistoryTime(raw json.RawMessage) (int64, bool) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int64(f), true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UnixMilli(), true
		}
	}
	return 0, false
}
