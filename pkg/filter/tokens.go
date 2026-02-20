package filter

type Token int

const (
	illegal Token = iota
	eol
	and
	or
	equal
	gte
	greater
	lte
	less
	notEqual
	like
	notLike
	lbracket
	rbracket
	stringLit
	regexLit
	quantity
	identifier
	boolean
)

var tokenNames = map[Token]string{
	illegal:    "illegal",
	eol:        "eol",
	and:        "and",
	equal:      "equal",
	gte:        "gte",
	greater:    "greater",
	lte:        "lte",
	less:       "less",
	or:         "or",
	notEqual:   "notEqual",
	like:       "like",
	notLike:    "notLike",
	stringLit:  "stringLit",
	regexLit:   "regexLit",
	quantity:   "quantity",
	lbracket:   "lbracket",
	rbracket:   "rbracket",
	identifier: "identifier",
	boolean:    "boolean",
}

func (t Token) String() string {
	return tokenNames[t]
}

var tokenSql = map[Token]string{
	and:      "AND",
	or:       "OR",
	equal:    "=",
	gte:      ">=",
	greater:  ">",
	lte:      "<=",
	less:     "<",
	notEqual: "!=",
	like:     "",    // translated to regexp_matches(...)
	notLike:  "NOT", // translated to NOT regexp_matches(...)
}

func (t Token) Sql() string {
	if sql, ok := tokenSql[t]; ok {
		return sql
	}
	return ""
}
