package termbrowser

import (
	"errors"
	"strings"
)

func SplitCommand(s string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	for index := 0; index < len(s); index++ {
		char := s[index]
		switch {
		case char == ' ' || char == '\t' || char == '\n':
			endWord()
		case char == '\\':
			index++
			if index == len(s) {
				return nil, errors.New("command ends with a lone backslash")
			}
			word.WriteByte(s[index])
			inWord = true
		case char == '\'':
			end := strings.IndexByte(s[index+1:], '\'')
			if end < 0 {
				return nil, errors.New("command has an unterminated single quote")
			}
			word.WriteString(s[index+1 : index+1+end])
			index += end + 1
			inWord = true
		case char == '"':
			next, err := readDoubleQuoted(s, index+1, &word)
			if err != nil {
				return nil, err
			}
			index = next
			inWord = true
		default:
			word.WriteByte(char)
			inWord = true
		}
	}
	endWord()
	return words, nil
}

func readDoubleQuoted(s string, start int, word *strings.Builder) (int, error) {
	for index := start; index < len(s); index++ {
		switch char := s[index]; {
		case char == '"':
			return index, nil
		case char == '\\' && index+1 < len(s) && (s[index+1] == '"' || s[index+1] == '\\'):
			index++
			word.WriteByte(s[index])
		default:
			word.WriteByte(char)
		}
	}
	return 0, errors.New("command has an unterminated double quote")
}
