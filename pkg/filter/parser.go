package filter

import (
	"fmt"
	"slices"
	"strings"

	sq "github.com/Masterminds/squirrel"
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

func ParseWithDefaultMap(src []byte) (sq.Sqlizer, error) {
	return Parse(src, defaultMapFn)
}

func ParseWithGroupMap(src []byte) (sq.Sqlizer, error) {
	return Parse(src, groupMapFn)
}

// Parse parses a filter expression and returns a Sqlizer that can be used with SelectBuilder.Where().
func Parse(src []byte, mf MapFunc) (sq.Sqlizer, error) {
	expr, err := parse(src)
	if err != nil {
		return nil, err
	}
	return toSql(expr, mf)
}

// parse uses panic/recover internally so recursive-descent methods can
// signal errors without threading (Expression, error) through every call.
// ParseError panics are caught here and returned as normal errors;
// any other panic (bug) is re-raised.
func parse(src []byte) (expr Expression, err error) {
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
// IDENTIFIER "in" "[" STRING ( "," STRING )* "]"
func (p *parser) equality() Expression {
	p.expect(identifier)
	left := &varExpression{Name: p.val}
	p.next()

	// Handle IN and NOT IN operators
	if p.tok == in {
		p.next()
		values := p.list()
		return &inExpression{Left: left, Values: values, Negated: false}
	}
	if p.tok == not {
		p.next()
		p.expect(in)
		p.next()
		values := p.list()
		return &inExpression{Left: left, Values: values, Negated: true}
	}

	var op Token
	switch p.tok {
	case equal, notEqual, greater, gte, less, lte:
		op = p.tok
		p.next()
	case like, notLike:
		op = p.tok
		p.next()
		p.expect(regexLit)
	case like2:
		op = p.tok
		p.next()
		p.expect(stringLit)
	default:
		panic(p.errorf(p.pos, "expected operator instead of %s", p.tok))
	}

	right := p.value()

	return &binaryExpression{Left: left, Op: op, Right: right}
}

// list parses a list of strings: "[" STRING ( "," STRING )* "]"
func (p *parser) list() []string {
	p.expect(lSquareBracket)
	p.next()

	var values []string

	// Handle empty list
	if p.tok == rSquareBracket {
		p.next()
		return values
	}

	// First value
	p.expect(stringLit)
	values = append(values, p.val)
	p.next()

	// Remaining values
	for p.tok == comma {
		p.next()
		p.expect(stringLit)
		values = append(values, p.val)
		p.next()
	}

	p.expect(rSquareBracket)
	p.next()

	return values
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
		panic(p.errorf(p.pos, "expected value instead of %s", p.tok))
	}

	p.next()
	return expr
}

// next parses the next token into p.tok.
func (p *parser) next() {
	pos, tok, val := p.lexer.Scan()
	p.pos, p.tok, p.val = pos, tok, val
	if tok == illegal {
		panic(p.errorf(pos, "%s", val))
	}
}

// matches returns true if current token matches one of the given tokens.
func (p *parser) matches(tokens ...Token) bool {
	return slices.Contains(tokens, p.tok)
}

// expect panics if current token is not the expected token.
func (p *parser) expect(tok Token) {
	if p.tok != tok {
		panic(p.errorf(p.pos, "expected %s instead of %s", tok, p.tok))
	}
}

// errorf formats an error with the given position.
func (p *parser) errorf(pos int, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	return ParseError{pos, message}
}
