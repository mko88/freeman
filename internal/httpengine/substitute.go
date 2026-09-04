// Package httpengine builds and executes an HTTP request from a saved
// domain.Item and a resolved set of environment variables. It depends
// only on domain.
package httpengine

import "regexp"

var varPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

// Substitute replaces {{key}} placeholders in text with values from vars.
// A placeholder with no matching key is left as-is, so a typo'd variable
// name is visible in the executed request instead of silently becoming an
// empty string.
func Substitute(text string, vars map[string]string) string {
	return varPattern.ReplaceAllStringFunc(text, func(match string) string {
		key := varPattern.FindStringSubmatch(match)[1]
		if val, ok := vars[key]; ok {
			return val
		}
		return match
	})
}
