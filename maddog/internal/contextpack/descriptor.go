package contextpack

import "strings"

type outputShape string

const (
	outputShapeUnknown        outputShape = "unknown"
	outputShapeText           outputShape = "text"
	outputShapeRipgrepMatches outputShape = "rg-matches"
	outputShapeStructured     outputShape = "structured"
	outputShapeGitStatusShort outputShape = "git-status-short"
	outputShapeGitStatusLong  outputShape = "git-status-long"
	outputShapeGitDiffPatch   outputShape = "git-diff-patch"
	outputShapeGoTestText     outputShape = "go-test-text"
	outputShapePackageText    outputShape = "package-script-text"
)

type commandSemantics struct {
	shell string
	goos  string
}

func newCommandSemantics(shell, goos string) commandSemantics {
	shell = strings.ToLower(strings.TrimSpace(shell))
	switch shell {
	case "shell":
		shell = "bash"
	case "pwsh":
		shell = "powershell"
	}
	return commandSemantics{
		shell: shell,
		goos:  strings.ToLower(strings.TrimSpace(goos)),
	}
}

func (s commandSemantics) supported() bool {
	return s.shell == "bash" || s.shell == "powershell"
}

// commandDescriptor is a conservative, single-pass view of a shell command.
// It is used only for output routing and never changes or executes a command.
type commandDescriptor struct {
	Shell       string
	GOOS        string
	Raw         string
	Segments    [][]string
	Tokens      []string
	Executable  string
	Subcommand  string
	Args        []string
	Flags       []string
	Compound    bool
	Redirected  bool
	Ambiguous   bool
	OutputShape outputShape
}

func describeCommand(semantics commandSemantics, raw string) commandDescriptor {
	desc := commandDescriptor{
		Shell: semantics.shell,
		GOOS:  semantics.goos,
		Raw:   strings.TrimSpace(raw),
	}
	var token strings.Builder
	var segment []string
	var quote byte
	tokenStarted := false
	flushToken := func() {
		if !tokenStarted {
			return
		}
		segment = append(segment, token.String())
		token.Reset()
		tokenStarted = false
	}
	flushSegment := func() {
		flushToken()
		if len(segment) == 0 {
			return
		}
		desc.Segments = append(desc.Segments, append([]string(nil), segment...))
		segment = nil
	}

	for i := 0; i < len(desc.Raw); i++ {
		ch := desc.Raw[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			if quote == '"' {
				if ch == '$' && i+1 < len(desc.Raw) && desc.Raw[i+1] == '(' {
					desc.Ambiguous = true
				}
				if desc.Shell == "powershell" && ch == '`' {
					if i+1 >= len(desc.Raw) {
						desc.Ambiguous = true
						continue
					}
					i++
					if desc.Raw[i] != '\n' {
						token.WriteByte(desc.Raw[i])
					}
					continue
				}
				if desc.Shell == "bash" && ch == '`' {
					desc.Ambiguous = true
				}
				if desc.Shell == "bash" && ch == '\\' && i+1 < len(desc.Raw) && isBashDoubleQuoteEscapable(desc.Raw[i+1]) {
					i++
					if desc.Raw[i] != '\n' {
						token.WriteByte(desc.Raw[i])
					}
					continue
				}
			}
			token.WriteByte(ch)
			continue
		}

		switch ch {
		case '\'', '"':
			quote = ch
			tokenStarted = true
		case ' ', '\t', '\r':
			flushToken()
		case '\n', ';', '|', '&':
			flushSegment()
			desc.Compound = true
			if i+1 < len(desc.Raw) && desc.Raw[i+1] == ch {
				i++
			}
		case '<', '>':
			flushToken()
			desc.Redirected = true
			if i+1 < len(desc.Raw) && desc.Raw[i+1] == ch {
				i++
			}
		case '`':
			tokenStarted = true
			if desc.Shell == "powershell" {
				if i+1 >= len(desc.Raw) {
					desc.Ambiguous = true
					continue
				}
				i++
				if desc.Raw[i] != '\n' {
					token.WriteByte(desc.Raw[i])
				}
				continue
			}
			desc.Ambiguous = true
			token.WriteByte(ch)
		case '$':
			if i+1 < len(desc.Raw) && desc.Raw[i+1] == '(' {
				desc.Ambiguous = true
			}
			tokenStarted = true
			token.WriteByte(ch)
		case '\\':
			tokenStarted = true
			if desc.Shell == "bash" && i+1 < len(desc.Raw) {
				i++
				if desc.Raw[i] != '\n' {
					token.WriteByte(desc.Raw[i])
				}
			} else {
				token.WriteByte(ch)
			}
		default:
			tokenStarted = true
			token.WriteByte(ch)
		}
	}
	flushSegment()
	if quote != 0 {
		desc.Ambiguous = true
	}
	if len(desc.Segments) == 1 {
		desc.Tokens = append([]string(nil), desc.Segments[0]...)
	}
	desc.resolveInvocation()
	return desc
}

func (d *commandDescriptor) resolveInvocation() {
	if d == nil || d.Compound || d.Redirected || d.Ambiguous || len(d.Tokens) == 0 {
		return
	}
	tokens := d.Tokens
	i := 0
	if d.Shell == "bash" {
		for i < len(tokens) && isEnvironmentAssignment(tokens[i]) {
			i++
		}
	}
	if i < len(tokens) && commandBaseName(tokens[i], d.GOOS) == "env" {
		i++
		for i < len(tokens) {
			switch {
			case tokens[i] == "--":
				i++
				goto resolved
			case tokens[i] == "-u" || tokens[i] == "--unset":
				i += 2
			case strings.HasPrefix(tokens[i], "--unset=") || isEnvironmentAssignment(tokens[i]):
				i++
			default:
				goto resolved
			}
		}
	}

resolved:
	if i >= len(tokens) {
		return
	}
	d.Executable = commandBaseName(tokens[i], d.GOOS)
	d.Args = append([]string(nil), tokens[i+1:]...)
	for _, arg := range d.Args {
		if strings.HasPrefix(arg, "-") {
			d.Flags = append(d.Flags, arg)
		}
	}

	switch d.Executable {
	case "git":
		d.Subcommand, _, _ = gitSubcommandParts(d.Args)
	case "go":
		if len(d.Args) > 0 {
			d.Subcommand = d.Args[0]
		}
	case "npm", "pnpm", "yarn":
		if len(d.Args) > 0 {
			d.Subcommand = d.Args[0]
			if d.Args[0] == "run" && len(d.Args) > 1 {
				d.Subcommand = d.Args[1]
			}
		}
	}
	d.OutputShape = detectOutputShape(*d)
}

func detectOutputShape(d commandDescriptor) outputShape {
	switch d.Executable {
	case "rg":
		if hasAnyFlag(d.Args,
			"--json", "--files", "--files-with-matches", "--files-without-match",
			"--count", "--count-matches", "--heading", "--stats", "--type-list",
			"--help", "--version", "-l", "-c") {
			return outputShapeStructured
		}
		return outputShapeRipgrepMatches
	case "git":
		_, args, ok := gitSubcommandParts(d.Args)
		if !ok {
			return outputShapeUnknown
		}
		switch d.Subcommand {
		case "status":
			if hasAnyFlag(args, "-z", "--null", "--porcelain=v2") {
				return outputShapeStructured
			}
			if hasAnyFlag(args, "-s", "--short", "--porcelain", "--porcelain=v1", "--porcelain=1") {
				return outputShapeGitStatusShort
			}
			return outputShapeGitStatusLong
		case "diff":
			if hasAnyFlag(args,
				"--raw", "--numstat", "--stat", "--shortstat", "--dirstat", "--dirstat-by-file",
				"--compact-summary", "--summary", "--name-only", "--name-status", "--check",
				"--binary", "--word-diff", "--word-diff=porcelain") {
				return outputShapeStructured
			}
			return outputShapeGitDiffPatch
		}
	case "go":
		if d.Subcommand == "test" {
			if hasAnyFlag(d.Args[1:], "-json", "-bench", "-fuzz", "-list") {
				return outputShapeStructured
			}
			return outputShapeGoTestText
		}
	case "npm", "pnpm", "yarn":
		if hasAnyFlag(d.Args, "--json", "--reporter=json", "--reporter=json-stream") {
			return outputShapeStructured
		}
		return outputShapePackageText
	}
	return outputShapeText
}

func gitSubcommandParts(args []string) (string, []string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-c" || arg == "-C" || arg == "--git-dir" || arg == "--work-tree" || arg == "--namespace":
			i++
			if i >= len(args) {
				return "", nil, false
			}
		case strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--git-dir=") ||
			strings.HasPrefix(arg, "--work-tree=") || strings.HasPrefix(arg, "--namespace="):
		case arg == "--no-pager" || arg == "--no-optional-locks" || arg == "--bare" || arg == "--literal-pathspecs":
		case strings.HasPrefix(arg, "-"):
			return "", nil, false
		default:
			return arg, args[i+1:], true
		}
	}
	return "", nil, false
}

func commandBaseName(value, goos string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		if index := strings.LastIndexAny(value, `/\`); index >= 0 {
			value = value[index+1:]
		}
		if strings.HasSuffix(strings.ToLower(value), ".exe") {
			value = value[:len(value)-4]
		}
		return strings.ToLower(value)
	}
	if index := strings.LastIndexByte(value, '/'); index >= 0 {
		value = value[index+1:]
	}
	return value
}

func isEnvironmentAssignment(token string) bool {
	index := strings.IndexByte(token, '=')
	if index <= 0 {
		return false
	}
	for i := 0; i < index; i++ {
		ch := token[i]
		if i == 0 && ch >= '0' && ch <= '9' {
			return false
		}
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

func isBashDoubleQuoteEscapable(ch byte) bool {
	return ch == '\\' || ch == '"' || ch == '$' || ch == '`' || ch == '\n'
}
