package pattern

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

func Match(pattern, relativePath, name string) (bool, error) {
	pattern = strings.ReplaceAll(pattern, "\\", "/")
	relativePath = strings.TrimPrefix(strings.ReplaceAll(relativePath, "\\", "/"), "./")
	value := name
	if strings.Contains(pattern, "/") {
		value = relativePath
	}
	if !strings.Contains(pattern, "**") {
		return path.Match(pattern, value)
	}
	expression, err := globRegexp(pattern)
	if err != nil {
		return false, err
	}
	return regexp.MatchString(expression, value)
}

func Validate(pattern string) error {
	if strings.Contains(pattern, "**") {
		_, err := globRegexp(strings.ReplaceAll(pattern, "\\", "/"))
		return err
	}
	_, err := path.Match(strings.ReplaceAll(pattern, "\\", "/"), "sample")
	return err
}

func globRegexp(glob string) (string, error) {
	var result strings.Builder
	result.WriteString("^")
	for i := 0; i < len(glob); {
		switch glob[i] {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i += 2
				if i < len(glob) && glob[i] == '/' {
					i++
					result.WriteString("(?:.*/)?")
				} else {
					result.WriteString(".*")
				}
			} else {
				i++
				result.WriteString("[^/]*")
			}
		case '?':
			i++
			result.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(glob[i+1:], ']')
			if end < 0 {
				return "", fmt.Errorf("unclosed character class in %q", glob)
			}
			end += i + 1
			result.WriteString(glob[i : end+1])
			i = end + 1
		default:
			result.WriteString(regexp.QuoteMeta(string(glob[i])))
			i++
		}
	}
	result.WriteString("$")
	return result.String(), nil
}
