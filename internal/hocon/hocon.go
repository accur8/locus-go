// Package hocon is a small, dependency-free parser for the subset of HOCON used
// by Locus configs. It supports: implicit root object; nested { } objects;
// [ ] arrays; quoted and unquoted scalars; dotted keys (a.b.c -> nested);
// ':', '=' or '{' member separators; comma and/or newline element separators;
// and '//' / '#' comments. Scalars are returned as strings (callers convert).
//
// Unlike strict HOCON, an unquoted value ends at end-of-line / , / } / ] and a
// comment only terminates a value when preceded by whitespace, so unquoted URLs
// like https://host/path parse intact.
package hocon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Value is one of: string, []Value, map[string]Value.
type Value interface{}

type parser struct {
	s []rune
	i int
	n int
}

// ParseFile reads and parses a HOCON file.
func ParseFile(path string) (map[string]Value, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(b))
}

// Parse parses HOCON text into a root object.
func Parse(text string) (map[string]Value, error) {
	p := &parser{s: []rune(text)}
	p.n = len(p.s)
	obj, err := p.parseMembers(false)
	if err != nil {
		return nil, err
	}
	p.skipWsAndComments()
	if p.i < p.n {
		return nil, fmt.Errorf("hocon: unexpected trailing input at %d: %q", p.i, p.around())
	}
	return obj, nil
}

func (p *parser) around() string {
	end := p.i + 20
	if end > p.n {
		end = p.n
	}
	return string(p.s[p.i:end])
}

func (p *parser) peek() rune {
	if p.i < p.n {
		return p.s[p.i]
	}
	return -1
}

func (p *parser) peekAt(off int) rune {
	if p.i+off < p.n {
		return p.s[p.i+off]
	}
	return -1
}

func isInlineWs(r rune) bool { return r == ' ' || r == '\t' || r == '\r' }
func isWs(r rune) bool       { return isInlineWs(r) || r == '\n' }

// skipWsAndComments skips whitespace (incl newlines) and //, # comments.
func (p *parser) skipWsAndComments() {
	for p.i < p.n {
		r := p.s[p.i]
		switch {
		case isWs(r):
			p.i++
		case r == '#':
			p.skipLine()
		case r == '/' && p.peekAt(1) == '/':
			p.skipLine()
		default:
			return
		}
	}
}

func (p *parser) skipLine() {
	for p.i < p.n && p.s[p.i] != '\n' {
		p.i++
	}
}

// parseMembers parses object members. If untilBrace, stops at the matching '}'
// (which it consumes); otherwise stops at EOF (root object).
func (p *parser) parseMembers(untilBrace bool) (map[string]Value, error) {
	obj := map[string]Value{}
	for {
		p.skipWsAndComments()
		if p.i >= p.n {
			if untilBrace {
				return nil, fmt.Errorf("hocon: unexpected EOF, missing '}'")
			}
			return obj, nil
		}
		if untilBrace && p.peek() == '}' {
			p.i++ // consume '}'
			return obj, nil
		}
		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		p.skipWsAndComments()
		var val Value
		switch p.peek() {
		case ':', '=':
			p.i++
			p.skipWsAndComments()
			val, err = p.parseValue()
		case '{':
			val, err = p.parseValue() // object value without separator
		default:
			return nil, fmt.Errorf("hocon: expected ':' '=' or '{' after key %q near %q", key, p.around())
		}
		if err != nil {
			return nil, err
		}
		setPath(obj, strings.Split(key, "."), val)
		p.consumeSeparators()
	}
}

// consumeSeparators eats inline ws, an optional comma, and trailing newlines.
func (p *parser) consumeSeparators() {
	for p.i < p.n && isInlineWs(p.s[p.i]) {
		p.i++
	}
	// trailing comment on the member's line
	if p.peek() == '#' || (p.peek() == '/' && p.peekAt(1) == '/') {
		p.skipLine()
	}
	if p.peek() == ',' {
		p.i++
	}
}

func (p *parser) parseKey() (string, error) {
	if p.peek() == '"' {
		return p.parseQuoted()
	}
	start := p.i
	for p.i < p.n {
		r := p.s[p.i]
		if isWs(r) || r == ':' || r == '=' || r == '{' {
			break
		}
		p.i++
	}
	if p.i == start {
		return "", fmt.Errorf("hocon: empty key near %q", p.around())
	}
	return string(p.s[start:p.i]), nil
}

func (p *parser) parseValue() (Value, error) {
	p.skipWsAndComments()
	switch p.peek() {
	case '{':
		p.i++ // consume '{'
		return p.parseMembers(true)
	case '[':
		return p.parseArray()
	case '"':
		return p.parseQuoted()
	case -1:
		return nil, fmt.Errorf("hocon: unexpected EOF expecting value")
	default:
		return p.parseUnquoted(), nil
	}
}

func (p *parser) parseArray() ([]Value, error) {
	p.i++ // consume '['
	arr := []Value{}
	for {
		p.skipWsAndComments()
		if p.i >= p.n {
			return nil, fmt.Errorf("hocon: unexpected EOF, missing ']'")
		}
		if p.peek() == ']' {
			p.i++
			return arr, nil
		}
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		// element separators: inline ws, optional comma, newlines/comments
		for p.i < p.n && isInlineWs(p.s[p.i]) {
			p.i++
		}
		if p.peek() == ',' {
			p.i++
		}
	}
}

func (p *parser) parseQuoted() (string, error) {
	p.i++ // consume opening quote
	var b strings.Builder
	for p.i < p.n {
		r := p.s[p.i]
		p.i++
		switch r {
		case '"':
			return b.String(), nil
		case '\\':
			if p.i >= p.n {
				return "", fmt.Errorf("hocon: bad escape at EOF")
			}
			e := p.s[p.i]
			p.i++
			switch e {
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			case 'r':
				b.WriteRune('\r')
			case '"':
				b.WriteRune('"')
			case '\\':
				b.WriteRune('\\')
			case '/':
				b.WriteRune('/')
			default:
				b.WriteRune(e)
			}
		default:
			b.WriteRune(r)
		}
	}
	return "", fmt.Errorf("hocon: unterminated string")
}

// parseUnquoted reads until end-of-line / , / } / ]; a trailing comment is only
// recognised when preceded by whitespace (so https:// URLs survive).
func (p *parser) parseUnquoted() string {
	start := p.i
	for p.i < p.n {
		r := p.s[p.i]
		if r == '\n' || r == ',' || r == '}' || r == ']' {
			break
		}
		// whitespace-preceded comment ends the value
		if isInlineWs(r) {
			if p.peekAt(1) == '#' || (p.peekAt(1) == '/' && p.peekAt(2) == '/') {
				break
			}
		}
		p.i++
	}
	return strings.TrimSpace(string(p.s[start:p.i]))
}

// setPath assigns value at the (possibly dotted) key path, creating/merging
// intermediate objects.
func setPath(obj map[string]Value, path []string, val Value) {
	for i := 0; i < len(path)-1; i++ {
		k := path[i]
		next, ok := obj[k].(map[string]Value)
		if !ok {
			next = map[string]Value{}
			obj[k] = next
		}
		obj = next
	}
	obj[path[len(path)-1]] = val
}

// ---- typed navigation helpers ----

// Lookup navigates a dotted path from root, returning nil if absent.
func Lookup(root map[string]Value, dotted string) Value {
	var cur Value = root
	for _, part := range strings.Split(dotted, ".") {
		m, ok := cur.(map[string]Value)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

// AsObject / AsArray / AsString are convenience type assertions.
func AsObject(v Value) (map[string]Value, bool) { m, ok := v.(map[string]Value); return m, ok }
func AsArray(v Value) ([]Value, bool)           { a, ok := v.([]Value); return a, ok }
func AsString(v Value) (string, bool)           { s, ok := v.(string); return s, ok }

// ToInt parses an int from a string value.
func ToInt(v Value) (int, bool) {
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

// ToBool parses a bool ("true"/"false", case-insensitive).
func ToBool(v Value) (bool, bool) {
	s, ok := v.(string)
	if !ok {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "on":
		return true, true
	case "false", "no", "off":
		return false, true
	}
	return false, false
}
