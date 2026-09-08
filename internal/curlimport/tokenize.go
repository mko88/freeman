package curlimport

import (
	"errors"
	"strconv"
	"strings"
)

// tokenize splits a pasted command the way a shell would, and forgives
// the things that survive a trip through a web page or a terminal:
//
//   - a leading `$` or `>` prompt
//   - line continuations, `\` on POSIX and `^` on cmd, and bare newlines
//     from a snippet that was wrapped without them
//   - smart quotes, which a documentation site will have substituted for
//     the straight ones curl needs
//
// Single quotes are literal, double quotes keep backslash escapes, and
// an unclosed quote is an error rather than a silent truncation — that
// one is worth telling the reader about, because it usually means they
// copied half a line.
func tokenize(text string) ([]string, error) {
	runes := []rune(normalize(text))
	var (
		args    []string
		current strings.Builder
		quoted  bool // current has been started, even if it's still empty
	)

	flush := func() {
		if quoted || current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
			quoted = false
		}
	}

	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\'':
			quoted = true
			j := indexRune(runes, i+1, '\'')
			if j < 0 {
				return nil, errors.New("unclosed ' in that command — it looks like part of a line is missing")
			}
			current.WriteString(string(runes[i+1 : j]))
			i = j

		case c == '"':
			quoted = true
			for i++; ; i++ {
				if i >= len(runes) {
					return nil, errors.New(`unclosed " in that command — it looks like part of a line is missing`)
				}
				if runes[i] == '"' {
					break
				}
				// Inside double quotes a backslash escapes only a few
				// characters; before anything else it is literal.
				if runes[i] == '\\' && i+1 < len(runes) && strings.ContainsRune(`"\$`+"`", runes[i+1]) {
					i++
				}
				current.WriteRune(runes[i])
			}

		case c == '\\' && i+1 < len(runes):
			// A continuation swallows the newline; anything else escapes
			// the next character.
			if runes[i+1] == '\n' || runes[i+1] == '\r' {
				continue
			}
			i++
			current.WriteRune(runes[i])

		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()

		default:
			current.WriteRune(c)
		}
	}
	flush()
	return args, nil
}

func normalize(text string) string {
	text = strings.TrimSpace(text)
	// A prompt copied along with the command.
	for _, prompt := range []string{"$ ", "> ", "PS> ", "# "} {
		text = strings.TrimPrefix(text, prompt)
	}
	// Smart quotes, non-breaking spaces and the odd zero-width character
	// a web page leaves behind.
	return strings.NewReplacer(
		"‘", "'", "’", "'",
		"“", `"`, "”", `"`,
		" ", " ", "​", "",
		// cmd's continuation, so a command copied from Windows docs
		// joins up the same way a POSIX one does.
		"^\n", "", "^\r\n", "",
		// PowerShell's.
		"`\n", "", "`\r\n", "",
	).Replace(text)
}

func indexRune(runes []rune, from int, target rune) int {
	for i := from; i < len(runes); i++ {
		if runes[i] == target {
			return i
		}
	}
	return -1
}

func atoi(s string) (int, error) { return strconv.Atoi(strings.TrimSpace(s)) }

func atof(s string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(s), 64) }
