package filter

import "strings"

type lexer struct {
	src     []byte
	ch      byte
	offset  int
	pos     int
	nextPos int
}

func newLexer(src []byte) *lexer {
	l := &lexer{src: src}
	l.next()

	return l
}

func (l *lexer) Scan() (int, Token, string) {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.next()
	}

	if l.ch == 0 {
		return l.pos, eol, ""
	}

	tok := illegal
	pos := l.pos
	val := ""

	ch := l.ch
	l.next()

	// keywords and identifiers
	if isIdentifierStart(ch) {
		start := l.tokenStart()
		for isIdentifierStart(l.ch) || isDot(l.ch) { // FIX: don't accept 2 dots
			l.next()
		}
		name := string(l.src[start:l.tokenEnd()])
		switch strings.ToLower(name) {
		case "and":
			tok = and
		case "or":
			tok = or
		case "true", "false":
			tok = boolean
			val = name
		default:
			tok = identifier
			val = name
		}
		return pos, tok, val
	}

	if isDigit(ch) {
		start := l.tokenStart()
		for isDigit(l.ch) || l.ch == '.' { // FIX: don't accept 2 dots
			l.next()
		}
		if isUnitStart(l.ch) {
			l.next()
			if l.ch != 'b' && l.ch != 'B' {
				return pos, illegal, "quantity unit is malformed"
			}
			l.next()
		}
		val = string(l.src[start:l.tokenEnd()])
		tok = quantity
		return pos, tok, val
	}

	switch ch {
	case '(':
		tok = lbracket
	case ')':
		tok = rbracket
	case '=':
		tok = equal
	case '~':
		tok = like
	case '!':
		switch l.ch {
		case '=':
			tok = notEqual
			l.next()
		case '~':
			tok = notLike
			l.next()
		default:
			tok = illegal
		}
	case '<':
		switch l.ch {
		case '=':
			tok = lte
			l.next()
		default:
			tok = less
		}
	case '>':
		switch l.ch {
		case '=':
			tok = gte
			l.next()
		default:
			tok = greater
		}
	case '"', '\'':
		chars := make([]byte, 0, 32)
		for l.ch != ch {
			if l.ch == 0 {
				return pos, illegal, "unclosed string"
			}
			chars = append(chars, l.ch)
			l.next()
		}
		l.next()
		if len(chars) == 0 {
			return pos, illegal, "empty string"
		}
		tok = stringLit
		val = string(chars)
	case '/':
		chars := make([]byte, 0, 32)
		for l.ch != '/' {
			if l.ch == 0 {
				return pos, illegal, "unclosed regex"
			}
			// handle escaped slash
			if l.ch == '\\' && l.offset < len(l.src) && l.src[l.offset] == '/' {
				l.next()
				chars = append(chars, '/')
			} else {
				chars = append(chars, l.ch)
			}
			l.next()
		}
		l.next()
		tok = regexLit
		val = string(chars)
	default:
		tok = illegal
		val = "unexpected char"
	}

	return pos, tok, val
}

// Load the next character into l.ch (or 0 on end of input) and update line position.
func (l *lexer) next() {
	l.pos = l.nextPos
	if l.offset >= len(l.src) {
		// For last character, move offset 1 past the end as it
		// simplifies offset calculations in NAME and NUMBER
		if l.ch != 0 {
			l.ch = 0
			l.offset++
			l.nextPos++
		}
		return
	}
	ch := l.src[l.offset]
	l.ch = ch
	l.nextPos++
	l.offset++
}

func isIdentifierStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isDot(ch byte) bool {
	return ch == '.'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

// tokenStart returns the start offset of the current token.
func (l *lexer) tokenStart() int {
	return l.offset - 2
}

// tokenEnd returns the end offset of the current token.
func (l *lexer) tokenEnd() int {
	return l.offset - 1
}

func isUnitStart(ch byte) bool {
	switch ch {
	case 'k', 'K':
		return true
	case 'm', 'M':
		return true
	case 'g', 'G':
		return true
	case 't', 'T':
		return true
	default:
		return false
	}
}
