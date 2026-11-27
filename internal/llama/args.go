package llama

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
)

type Args []string

func (a *Args) UnmarshalJSON(b []byte) error {
	var line string
	if json.Unmarshal(b, &line) == nil {
		words, err := SplitArgs(line)
		*a = words
		return err
	}
	return json.Unmarshal(b, (*[]string)(a))
}

func (a Args) String() string { return Shell(a) }

func SplitArgs(s string) (Args, error) {
	var out Args
	var word strings.Builder
	var quote rune
	inWord, escaped := false, false
	for _, r := range s {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case quote == '\'' && r != '\'', quote == '"' && r != '"' && r != '\\':
			word.WriteRune(r)
		case quote != 0 && r == quote:
			quote = 0
		case r == '\\':
			escaped, inWord = true, true
		case quote == 0 && (r == '\'' || r == '"'):
			quote, inWord = r, true
		case unicode.IsSpace(r):
			if inWord {
				out = append(out, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("balanced quotes")
	}
	if inWord {
		out = append(out, word.String())
	}
	return out, nil
}

func isFlag(w string) bool {
	_, err := strconv.ParseFloat(w, 64)
	return strings.HasPrefix(w, "-") && err != nil
}
