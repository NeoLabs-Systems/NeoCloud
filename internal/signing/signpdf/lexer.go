package signpdf

import (
	"bytes"
	"strconv"
)

// A tokenizer for content streams and CMaps. Operands come out as float64,
// pdfString, pdfName, []any or nil (dictionaries, which nothing here needs);
// operators as pdfOp. It never fails: damaged input ends the stream early.

type (
	pdfString []byte
	pdfName   string
	pdfOp     string
)

type lexer struct {
	data []byte
	pos  int
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

func isDelimiter(c byte) bool {
	return bytes.IndexByte([]byte("()<>[]{}/%"), c) >= 0
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.data) {
		c := l.data[l.pos]
		if c == '%' {
			for l.pos < len(l.data) && l.data[l.pos] != '\n' && l.data[l.pos] != '\r' {
				l.pos++
			}
			continue
		}
		if !isSpace(c) {
			return
		}
		l.pos++
	}
}

// next returns the next operand or operator; ok is false at the end.
func (l *lexer) next() (value any, ok bool) {
	return l.object(0)
}

func (l *lexer) object(depth int) (any, bool) {
	l.skipSpace()
	if l.pos >= len(l.data) || depth > 32 {
		return nil, false
	}
	c := l.data[l.pos]
	switch {
	case c == '(':
		l.pos++
		return l.literal(), true
	case c == '<' && l.pos+1 < len(l.data) && l.data[l.pos+1] == '<':
		l.pos += 2
		for {
			l.skipSpace()
			if l.pos+1 < len(l.data) && l.data[l.pos] == '>' && l.data[l.pos+1] == '>' {
				l.pos += 2
				return nil, true
			}
			if _, ok := l.object(depth + 1); !ok {
				return nil, false
			}
		}
	case c == '<':
		l.pos++
		return l.hex(), true
	case c == '[':
		l.pos++
		var items []any
		for {
			l.skipSpace()
			if l.pos >= len(l.data) {
				return items, true
			}
			if l.data[l.pos] == ']' {
				l.pos++
				return items, true
			}
			item, ok := l.object(depth + 1)
			if !ok {
				return items, true
			}
			if _, op := item.(pdfOp); !op {
				items = append(items, item)
			}
		}
	case c == '/':
		l.pos++
		start := l.pos
		for l.pos < len(l.data) && !isSpace(l.data[l.pos]) && !isDelimiter(l.data[l.pos]) {
			l.pos++
		}
		return pdfName(decodeName(l.data[start:l.pos])), true
	case c == ']' || c == '>' || c == ')' || c == '{' || c == '}':
		l.pos++
		return l.object(depth)
	case c == '+' || c == '-' || c == '.' || c >= '0' && c <= '9':
		start := l.pos
		l.pos++
		for l.pos < len(l.data) && (l.data[l.pos] == '.' || l.data[l.pos] >= '0' && l.data[l.pos] <= '9') {
			l.pos++
		}
		n, err := strconv.ParseFloat(string(l.data[start:l.pos]), 64)
		if err != nil {
			return 0.0, true
		}
		return n, true
	}
	start := l.pos
	for l.pos < len(l.data) && !isSpace(l.data[l.pos]) && !isDelimiter(l.data[l.pos]) {
		l.pos++
	}
	op := string(l.data[start:l.pos])
	if op == "BI" {
		l.skipInlineImage()
	}
	return pdfOp(op), true
}

func decodeName(raw []byte) string {
	if bytes.IndexByte(raw, '#') < 0 {
		return string(raw)
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' && i+2 < len(raw) {
			if v, err := strconv.ParseUint(string(raw[i+1:i+3]), 16, 8); err == nil {
				out = append(out, byte(v))
				i += 2
				continue
			}
		}
		out = append(out, raw[i])
	}
	return string(out)
}

func (l *lexer) literal() pdfString {
	var out []byte
	depth := 1
	for l.pos < len(l.data) {
		c := l.data[l.pos]
		l.pos++
		switch c {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return out
			}
		case '\\':
			if l.pos >= len(l.data) {
				return out
			}
			e := l.data[l.pos]
			l.pos++
			switch e {
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case '\r':
				if l.pos < len(l.data) && l.data[l.pos] == '\n' {
					l.pos++
				}
				continue
			case '\n':
				continue
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for i := 0; i < 2 && l.pos < len(l.data) && l.data[l.pos] >= '0' && l.data[l.pos] <= '7'; i++ {
						v = v*8 + int(l.data[l.pos]-'0')
						l.pos++
					}
					c = byte(v)
				} else {
					c = e
				}
			}
		}
		out = append(out, c)
	}
	return out
}

func (l *lexer) hex() pdfString {
	var out []byte
	high, half := byte(0), false
	for l.pos < len(l.data) {
		c := l.data[l.pos]
		l.pos++
		var v byte
		switch {
		case c == '>':
			if half {
				out = append(out, high<<4)
			}
			return out
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		default:
			continue
		}
		if half {
			out = append(out, high<<4|v)
		} else {
			high = v
		}
		half = !half
	}
	return out
}

// skipInlineImage moves past the data of an inline image (BI … ID data EI).
func (l *lexer) skipInlineImage() {
	index := bytes.Index(l.data[l.pos:], []byte("ID"))
	if index < 0 {
		l.pos = len(l.data)
		return
	}
	l.pos += index + 3
	for l.pos+1 < len(l.data) {
		if l.data[l.pos] == 'E' && l.data[l.pos+1] == 'I' && isSpace(l.data[l.pos-1]) && (l.pos+2 == len(l.data) || isSpace(l.data[l.pos+2])) {
			l.pos += 2
			return
		}
		l.pos++
	}
	l.pos = len(l.data)
}
