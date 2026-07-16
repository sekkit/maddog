package hook

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	fileencoding "maddog/internal/fileutil/encoding"
	"maddog/internal/sandbox"
)

// expandPluginRoot performs one-pass host expansion. MADDOG is canonical;
// CLAUDE remains a compatibility alias for imported plugin manifests.
func expandPluginRoot(value, root string) string {
	if root == "" {
		return value
	}
	tokens := []struct {
		value    string
		boundary bool
	}{
		{"${MADDOG_PLUGIN_ROOT}", false}, {"$MADDOG_PLUGIN_ROOT", true}, {"%MADDOG_PLUGIN_ROOT%", false},
		{"${CLAUDE_PLUGIN_ROOT}", false}, {"$CLAUDE_PLUGIN_ROOT", true}, {"%CLAUDE_PLUGIN_ROOT%", false},
	}
	var out strings.Builder
	last := 0
	changed := false
	for i := 0; i < len(value); {
		matched := ""
		for _, token := range tokens {
			if strings.HasPrefix(value[i:], token.value) && (!token.boundary || len(value) == i+len(token.value) || !isShellVariableNameByte(value[i+len(token.value)])) {
				matched = token.value
				break
			}
		}
		if matched == "" {
			i++
			continue
		}
		if !changed {
			out.Grow(len(value) - len(matched) + len(root))
			changed = true
		}
		out.WriteString(value[last:i])
		out.WriteString(root)
		i += len(matched)
		last = i
	}
	if !changed {
		return value
	}
	out.WriteString(value[last:])
	return out.String()
}

func isShellVariableNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isBarePOSIXShellWord(word string) bool {
	word = strings.ToLower(strings.TrimSpace(word))
	return !strings.ContainsAny(word, `/\\:`) && (word == "sh" || word == "sh.exe" || word == "bash" || word == "bash.exe")
}

func hasCommandStringFlag(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || arg == "-" || !strings.HasPrefix(arg, "-") {
			return false
		}
		if strings.HasPrefix(arg, "--") {
			name, _, inline := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			if !inline && (name == "rcfile" || name == "init-file") {
				if i+1 >= len(args) {
					return false
				}
			}
			continue
		}
		flags := strings.TrimPrefix(arg, "-")
		for j := 0; j < len(flags); j++ {
			flag := flags[j]
			switch flag {
			case 'c':
				return i+1 < len(args)
			case 'o', 'O':
				if j+1 == len(flags) {
					if i+1 >= len(args) {
						return false
					}
					i++
				}
				j = len(flags)
			}
		}
	}
	return false
}

func windowsPOSIXShellInvocation(command string) (string, []string, bool, error) {
	fields, ok := parseHookShellFields(command)
	if !ok || len(fields) < 3 || !isBarePOSIXShellWord(fields[0]) || !hasCommandStringFlag(fields[1:]) {
		return "", nil, false, nil
	}
	path, err := resolveWindowsHookBash()
	if err != nil {
		return "", nil, true, err
	}
	return path, fields[1:], true, nil
}

func parseHookShellFields(command string) ([]string, bool) {
	if strings.ContainsAny(command, "\r\n") {
		return nil, false
	}
	var fields []string
	for i := 0; i < len(command); {
		for i < len(command) && (command[i] == ' ' || command[i] == '\t') {
			i++
		}
		if i == len(command) {
			break
		}
		var b strings.Builder
		var quote byte
		for i < len(command) {
			c := command[i]
			if quote == 0 {
				if c == ' ' || c == '\t' {
					break
				}
				if strings.ContainsRune(";&|<>()`", rune(c)) {
					return nil, false
				}
				if c == '\'' || c == '"' {
					quote = c
					i++
					continue
				}
				b.WriteByte(c)
				i++
				continue
			}
			if c == quote {
				quote = 0
				i++
				continue
			}
			if quote == '"' && c == '\\' && i+1 < len(command) {
				next := command[i+1]
				if next == '$' || next == '`' || next == '"' || next == '\\' {
					b.WriteByte(next)
					i += 2
					continue
				}
			}
			b.WriteByte(c)
			i++
		}
		if quote != 0 {
			return nil, false
		}
		fields = append(fields, b.String())
	}
	return fields, len(fields) > 0
}

func windowsPOSIXShellArgvInvocation(command string, args []string) (string, []string, bool, error) {
	if !isBarePOSIXShellWord(command) || !hasCommandStringFlag(args) {
		return "", nil, false, nil
	}
	path, err := resolveWindowsHookBash()
	if err != nil {
		return "", nil, true, err
	}
	return path, append([]string(nil), args...), true, nil
}

func resolveWindowsHookBash() (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("Git Bash discovery is Windows-only")
	}
	shell := sandbox.ResolveShell("bash", "", nil)
	if shell.Kind == sandbox.ShellBash && strings.TrimSpace(shell.Path) != "" {
		if path, err := exec.LookPath(shell.Path); err == nil {
			return path, nil
		}
		if filepath.IsAbs(shell.Path) {
			if info, err := os.Stat(shell.Path); err == nil && !info.IsDir() {
				return shell.Path, nil
			}
		}
	}
	return "", errors.New("hook requires a POSIX shell on Windows, but no usable Git Bash was found; install Git for Windows or use a native command")
}

func decodeHookOutput(raw []byte, truncated bool) string {
	if len(raw) == 0 {
		return ""
	}
	decoded := raw
	if !utf8.Valid(raw) {
		if truncated {
			for n := 1; n < utf8.UTFMax && n < len(raw); n++ {
				if utf8.Valid(raw[:len(raw)-n]) && !utf8.FullRune(raw[len(raw)-n:]) {
					decoded = raw[:len(raw)-n]
					break
				}
			}
		}
		if !utf8.Valid(decoded) {
			decoded = fileencoding.Decode(decoded, fileencoding.GB18030)
		}
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(decoded), "\uFFFD"))
}
