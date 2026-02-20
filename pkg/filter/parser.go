package filter

import (
	"fmt"
	"slices"
	"strings"
)

// ParseError is the type of error returned by parse.
type ParseError struct {
	// Source column position where the error occurred.
	Position int
	// Error message.
	Message string
}

// Error returns a formatted version of the error, including the position.
func (e ParseError) Error() string {
	return fmt.Sprintf("parse error at %d: %s", e.Position, e.Message)
}

type parser struct {
	lexer *lexer
	pos   int    // position of last token (tok)
	tok   Token  // last lexed token
	val   string // string value of last token (or "")
}

// Parse uses panic/recover internally so recursive-descent methods can
// signal errors without threading (Expression, error) through every call.
// ParseError panics are caught here and returned as normal errors;
// any other panic (bug) is re-raised.
// The top level expression is returned as an Expression, and any errors are returned as a ParseError.
func Parse(src []byte) (expr Expression, err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(ParseError); ok {
				expr = nil
				err = pe
			} else {
				panic(r)
			}
		}
	}()

	lexer := newLexer(src)
	p := parser{lexer: lexer}
	p.next()

	expr = p.expression()
	p.expect(eol)

	return expr, err
}

// expression parses a logic expression.
//
// term ( "or" term )*
func (p *parser) expression() Expression {
	expr := p.term()

	for p.matches(or) {
		op := p.tok
		p.next()
		right := p.term()
		expr = &binaryExpression{Left: expr, Op: op, Right: right}
	}

	return expr
}

// term parses an AND expression.
//
// factor ( "and" factor )*
func (p *parser) term() Expression {
	expr := p.factor()

	for p.matches(and) {
		op := p.tok
		p.next()
		right := p.factor()
		expr = &binaryExpression{Left: expr, Op: op, Right: right}
	}

	return expr
}

// factor parses a single comparison or grouped expression.
//
// equality | "(" expression ")"
func (p *parser) factor() Expression {
	if p.matches(lbracket) {
		p.next()
		expr := p.expression()
		p.expect(rbracket)
		p.next()
		return expr
	}

	return p.equality()
}

// equality parses a comparison expression.
//
// IDENTIFIER ( "=" | "!=" | "<" | "<=" | ">" | ">=" | "~" | "!~" ) value
func (p *parser) equality() Expression {
	p.expect(identifier)
	left := &varExpression{Name: p.val}
	p.next()

	var op Token
	switch p.tok {
	case equal, notEqual, greater, gte, less, lte, like, notLike:
		op = p.tok
		p.next()
	default:
		panic(p.errorf("expected operator instead of %s", p.tok))
	}

	right := p.value()

	return &binaryExpression{Left: left, Op: op, Right: right}
}

// value parses a value (string, quantity, boolean, or regex).
func (p *parser) value() Expression {
	var expr Expression

	switch p.tok {
	case stringLit:
		expr = &stringExpression{Value: p.val}
	case quantity:
		expr = newQuantityExpression(p.val)
	case boolean:
		expr = &booleanExpression{Value: strings.EqualFold(p.val, "true")}
	case regexLit:
		expr = newRegexExpression(p.pos, p.val)
	default:
		panic(p.errorf("expected value instead of %s", p.tok))
	}

	p.next()
	return expr
}

// next parses the next token into p.tok.
func (p *parser) next() {
	p.pos, p.tok, p.val = p.lexer.Scan()
	if p.tok == illegal {
		panic(p.errorf("%s", p.val))
	}
}

// matches returns true if current token matches one of the given tokens.
func (p *parser) matches(tokens ...Token) bool {
	return slices.Contains(tokens, p.tok)
}

// expect panics if current token is not the expected token.
func (p *parser) expect(tok Token) {
	if p.tok != tok {
		panic(p.errorf("expected %s instead of %s", tok, p.tok))
	}
}

// errorf formats an error with the current position.
func (p *parser) errorf(format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	return ParseError{p.pos, message}
}
