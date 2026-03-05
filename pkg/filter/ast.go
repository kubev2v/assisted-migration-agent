package filter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type QuantityUnit int

func (q QuantityUnit) String() string {
	switch q {
	case KbQuantityUnit:
		return "Kb"
	case MbQuantityUnit:
		return "Mb"
	case GbQuantityUnit:
		return "Gb"
	case TbQuantityUnit:
		return "Tb"
	case NoQuantityUnit:
		return "noUnit"
	default:
		return "unknown"
	}
}

const (
	NoQuantityUnit QuantityUnit = iota
	KbQuantityUnit
	MbQuantityUnit // this is the baseline. In db, we store as Mb
	GbQuantityUnit
	TbQuantityUnit
)

// Expression is the abstract syntax tree for any expression.
type Expression interface {
	String() string
}

// binaryExpression is an expression like "a = b" or "a and b".
type binaryExpression struct {
	Left  Expression
	Op    Token
	Right Expression
}

func (e *binaryExpression) String() string {
	return fmt.Sprintf("(%s %s %s)", e.Left.String(), e.Op.String(), e.Right.String())
}

// stringExpression is a literal string like "foo".
type stringExpression struct {
	Value string
}

func (e *stringExpression) String() string {
	return strconv.Quote(e.Value)
}

// varExpression is a variable/identifier like "vm_id" or "primary_ip_address".
type varExpression struct {
	Name string
}

func (v *varExpression) String() string {
	return v.Name
}

// booleanExpression is a boolean literal (true or false).
type booleanExpression struct {
	Value bool
}

func (b *booleanExpression) String() string {
	return strconv.FormatBool(b.Value)
}

// regexExpression is a regex literal like /pattern/.
type regexExpression struct {
	Pattern string
}

func newRegexExpression(pos int, pattern string) *regexExpression {
	if _, err := regexp.Compile(pattern); err != nil {
		panic(ParseError{pos, fmt.Sprintf("invalid regex: %s", err)})
	}
	return &regexExpression{Pattern: pattern}
}

func (r *regexExpression) String() string {
	return fmt.Sprintf("/%s/", r.Pattern)
}

type quantityExpression struct {
	Value float64
	Unit  QuantityUnit
}

func newQuantityExpression(val string) *quantityExpression {
	qe := &quantityExpression{Unit: NoQuantityUnit}

	numStr := val
	if len(val) >= 3 {
		suffix := strings.ToLower(val[len(val)-2:])
		switch suffix {
		case "kb":
			qe.Unit = KbQuantityUnit
			numStr = val[:len(val)-2]
		case "mb":
			qe.Unit = MbQuantityUnit
			numStr = val[:len(val)-2]
		case "gb":
			qe.Unit = GbQuantityUnit
			numStr = val[:len(val)-2]
		case "tb":
			qe.Unit = TbQuantityUnit
			numStr = val[:len(val)-2]
		default:
			qe.Unit = NoQuantityUnit
		}
	}

	qe.Value, _ = strconv.ParseFloat(numStr, 64)
	return qe
}

func (q *quantityExpression) String() string {
	if q.Unit == NoQuantityUnit {
		return fmt.Sprintf("%.2f", q.Value)
	}
	return fmt.Sprintf("%.2f%s", q.Value, q.Unit)
}

// inExpression is an expression like "field IN ['a', 'b']" or "field NOT IN ['a', 'b']".
type inExpression struct {
	Left    Expression
	Values  []string
	Negated bool
}

func (e *inExpression) String() string {
	quoted := make([]string, len(e.Values))
	for i, v := range e.Values {
		quoted[i] = strconv.Quote(v)
	}
	op := "IN"
	if e.Negated {
		op = "NOT IN"
	}
	return fmt.Sprintf("(%s %s [%s])", e.Left.String(), op, strings.Join(quoted, ", "))
}
