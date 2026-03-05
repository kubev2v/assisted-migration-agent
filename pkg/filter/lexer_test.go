package filter

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lexer", func() {
	Context("Scan", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== OPERATORS =====
			// Equality operators
			{input: "=", output: "equal eol"},
			{input: "!=", output: "notEqual eol"},

			// Comparison operators
			{input: "<", output: "less eol"},
			{input: "<=", output: "lte eol"},
			{input: ">", output: "greater eol"},
			{input: ">=", output: "gte eol"},

			// Regex operators
			{input: "~", output: "like eol"},
			{input: "!~", output: "notLike eol"},

			// All operators together
			{input: "= != < <= > >= ~ !~", output: "equal notEqual less lte greater gte like notLike eol"},

			// ===== LOGICAL OPERATORS =====
			{input: "and", output: "and eol"},
			{input: "or", output: "or eol"},
			{input: "AND", output: "and eol"},
			{input: "OR", output: "or eol"},
			{input: "And", output: "and eol"},
			{input: "Or", output: "or eol"},
			{input: "and or and", output: "and or and eol"},

			// ===== BRACKETS =====
			{input: "(", output: "lbracket eol"},
			{input: ")", output: "rbracket eol"},
			{input: "()", output: "lbracket rbracket eol"},
			{input: "( )", output: "lbracket rbracket eol"},

			// ===== SQUARE BRACKETS AND COMMA =====
			{input: "[", output: "[ eol"},
			{input: "]", output: "] eol"},
			{input: "[]", output: "[ ] eol"},
			{input: ",", output: ", eol"},
			{input: "[ , ]", output: "[ , ] eol"},

			// ===== IN OPERATOR =====
			{input: "in", output: "in eol"},
			{input: "IN", output: "in eol"},
			{input: "In", output: "in eol"},
			{input: "status in ['a', 'b']", output: "identifier in [ stringLit , stringLit ] eol"},

			// ===== NOT IN OPERATOR =====
			{input: "not", output: "not eol"},
			{input: "NOT", output: "not eol"},
			{input: "Not", output: "not eol"},
			{input: "not in", output: "not in eol"},
			{input: "status not in ['a', 'b']", output: "identifier not in [ stringLit , stringLit ] eol"},

			// ===== STRINGS =====
			// Single quoted strings
			{input: "'test'", output: "stringLit eol"},
			{input: "'hello world'", output: "stringLit eol"},
			{input: "''", output: "stringLit eol"}, // empty string allowed

			// Double quoted strings
			{input: `"test"`, output: "stringLit eol"},
			{input: `"hello world"`, output: "stringLit eol"},
			{input: `""`, output: "stringLit eol"}, // empty string allowed

			// Mixed quotes
			{input: `'test' "test"`, output: "stringLit stringLit eol"},

			// Strings with special characters
			{input: "'test=value'", output: "stringLit eol"},
			{input: "'test>value'", output: "stringLit eol"},
			{input: "'test<value'", output: "stringLit eol"},
			{input: `"with spaces and symbols !@#$%"`, output: "stringLit eol"},

			// Escaped quotes inside strings
			{input: `'it\'s'`, output: "stringLit eol"},
			{input: `"say \"hello\""`, output: "stringLit eol"},
			{input: `'no escape needed "here"'`, output: "stringLit eol"},
			{input: `"no escape needed 'here'"`, output: "stringLit eol"},

			// ===== REGEX LITERALS =====
			{input: "/pattern/", output: "regexLit eol"},
			{input: "/hello world/", output: "regexLit eol"},
			{input: "//", output: "regexLit eol"},            // empty regex
			{input: "/test\\/path/", output: "regexLit eol"}, // escaped slash
			{input: "/^[a-z]+$/", output: "regexLit eol"},
			{input: "/.*prod.*/", output: "regexLit eol"},

			// ===== BOOLEANS =====
			{input: "true", output: "boolean eol"},
			{input: "false", output: "boolean eol"},
			{input: "TRUE", output: "boolean eol"},
			{input: "FALSE", output: "boolean eol"},
			{input: "True", output: "boolean eol"},
			{input: "False", output: "boolean eol"},

			// ===== QUANTITIES =====
			// With units
			{input: "100KB", output: "quantity eol"},
			{input: "100kb", output: "quantity eol"},
			{input: "50MB", output: "quantity eol"},
			{input: "50mb", output: "quantity eol"},
			{input: "8GB", output: "quantity eol"},
			{input: "8gb", output: "quantity eol"},
			{input: "2TB", output: "quantity eol"},
			{input: "2tb", output: "quantity eol"},
			{input: "1.5GB", output: "quantity eol"},
			{input: "100.25MB", output: "quantity eol"},
			{input: "0.5TB", output: "quantity eol"},

			// Without units (plain numbers)
			{input: "100", output: "quantity eol"},
			{input: "0", output: "quantity eol"},
			{input: "42", output: "quantity eol"},
			{input: "3.14", output: "quantity eol"},
			{input: "0.5", output: "quantity eol"},
			{input: "100.25", output: "quantity eol"},

			// ===== IDENTIFIERS / VARIABLES =====
			// Simple identifiers
			{input: "name", output: "identifier eol"},
			{input: "Name", output: "identifier eol"},
			{input: "NAME", output: "identifier eol"},
			{input: "description", output: "identifier eol"},

			// Dotted identifiers (nested fields)
			{input: "user.name", output: "identifier eol"},
			{input: "vm.host.datacenter", output: "identifier eol"},
			{input: "a.b.c.d.e", output: "identifier eol"},

			// Multiple identifiers
			{input: "name description", output: "identifier identifier eol"},

			// ===== WHITESPACE HANDLING =====
			{input: "", output: "eol"},
			{input: "   ", output: "eol"},
			{input: "\t\t", output: "eol"},
			{input: "  name  ", output: "identifier eol"},
			{input: "\tname\t", output: "identifier eol"},
			{input: "name   =   'test'", output: "identifier equal stringLit eol"},

			// ===== COMPLETE FILTER EXPRESSIONS =====
			// Simple equality
			{input: "name = 'test'", output: "identifier equal stringLit eol"},
			{input: "name != 'test'", output: "identifier notEqual stringLit eol"},

			// Comparison expressions
			{input: "count > '10'", output: "identifier greater stringLit eol"},
			{input: "count >= '10'", output: "identifier gte stringLit eol"},
			{input: "count < '10'", output: "identifier less stringLit eol"},
			{input: "count <= '10'", output: "identifier lte stringLit eol"},

			// AND expressions
			{input: "name = 'test' and status = 'active'", output: "identifier equal stringLit and identifier equal stringLit eol"},
			{input: "a = '1' and b = '2' and c = '3'", output: "identifier equal stringLit and identifier equal stringLit and identifier equal stringLit eol"},

			// OR expressions
			{input: "name = 'test' or status = 'active'", output: "identifier equal stringLit or identifier equal stringLit eol"},
			{input: "a = '1' or b = '2' or c = '3'", output: "identifier equal stringLit or identifier equal stringLit or identifier equal stringLit eol"},

			// Mixed AND/OR expressions
			{input: "name = 'test' and status = 'active' or location = 'us'", output: "identifier equal stringLit and identifier equal stringLit or identifier equal stringLit eol"},
			{input: "a = '1' or b = '2' and c = '3'", output: "identifier equal stringLit or identifier equal stringLit and identifier equal stringLit eol"},

			// Complex nested field expressions
			{input: "vm.host.name = 'host1' and vm.status = 'running'", output: "identifier equal stringLit and identifier equal stringLit eol"},

			// ===== EDGE CASES =====
			// Operators without spaces
			{input: "name='test'", output: "identifier equal stringLit eol"},
			{input: "count>='10'", output: "identifier gte stringLit eol"},
			{input: "count<='10'", output: "identifier lte stringLit eol"},

			// Keywords as part of identifiers (should be identifiers, not keywords)
			{input: "android", output: "identifier eol"},
			{input: "organic", output: "identifier eol"},
			{input: "indoor", output: "identifier eol"},
			{input: "origin", output: "identifier eol"},

			// Consecutive dots in identifiers
			{input: "a..b", output: "illegal illegal identifier eol"},
			{input: "vm..name", output: "illegal illegal identifier eol"},
			{input: "a.b..c", output: "illegal illegal identifier eol"},

			// Trailing dot in identifiers
			{input: "name.", output: "illegal eol"},
			{input: "vm.name.", output: "illegal eol"},

			// Multiple decimal points in numbers
			{input: "3.14.15", output: "illegal illegal quantity eol"},
			{input: "1.2.3", output: "illegal illegal quantity eol"},
			{input: "100.25.5", output: "illegal illegal quantity eol"},

			// ===== ILLEGAL TOKENS =====
			{input: "!", output: "illegal eol"},  // incomplete != or !~
			{input: "@", output: "illegal eol"},  // unsupported character
			{input: "#", output: "illegal eol"},  // unsupported character
			{input: "$", output: "illegal eol"},  // unsupported character
			{input: "%", output: "illegal eol"},  // unsupported character
			{input: "^", output: "illegal eol"},  // unsupported character
			{input: "&", output: "illegal eol"},  // unsupported character
			{input: "*", output: "illegal eol"},  // unsupported character
			{input: "`", output: "illegal eol"},  // unsupported character
			{input: "\\", output: "illegal eol"}, // unsupported character
			{input: "|", output: "illegal eol"},  // unsupported character
			{input: ";", output: "illegal eol"},  // unsupported character
			{input: ":", output: "illegal eol"},  // unsupported character

			// Unclosed strings
			{input: "'unclosed", output: "illegal eol"},
			{input: `"unclosed`, output: "illegal eol"},

			// Unclosed regex
			{input: "/unclosed", output: "illegal eol"},

			// ===== REAL-WORLD FILTER EXAMPLES =====
			{
				input:  "vm.name = 'production-db' and vm.host.datacenter = 'DC1' or vm.status = 'migrating'",
				output: "identifier equal stringLit and identifier equal stringLit or identifier equal stringLit eol",
			},
			{
				input:  "memory <= '8GB' or cpu.cores > '4'",
				output: "identifier lte stringLit or identifier greater stringLit eol",
			},
			{
				input:  "os.name = 'linux' and os.version != 'ubuntu' and kernel.version >= '5.0'",
				output: "identifier equal stringLit and identifier notEqual stringLit and identifier gte stringLit eol",
			},

			// Regex expressions
			{
				input:  "vm.name ~ /^prod-.*/",
				output: "identifier like regexLit eol",
			},
			{
				input:  "vm.name !~ /test/",
				output: "identifier notLike regexLit eol",
			},
			{
				input:  "name ~ /pattern/ and status = 'active'",
				output: "identifier like regexLit and identifier equal stringLit eol",
			},

			// Boolean expressions
			{
				input:  "active = true",
				output: "identifier equal boolean eol",
			},
			{
				input:  "enabled = false and visible = true",
				output: "identifier equal boolean and identifier equal boolean eol",
			},

			// Mixed types
			{
				input:  "name ~ /prod/ and enabled = true and count > '10'",
				output: "identifier like regexLit and identifier equal boolean and identifier greater stringLit eol",
			},
		}

		for _, test := range tests {
			test := test // capture range variable
			It("should tokenize: "+test.input, func() {
				l := newLexer([]byte(test.input))

				tokens := []string{}
				for {
					_, tok, _ := l.Scan()
					tokens = append(tokens, tok.String())
					if tok == eol {
						break
					}
				}

				output := strings.Join(tokens, " ")
				Expect(strings.TrimSpace(output)).To(Equal(test.output))
			})
		}
	})

	Context("escaped string values", func() {
		It("should unescape single quotes", func() {
			l := newLexer([]byte(`'it\'s'`))
			_, tok, val := l.Scan()
			Expect(tok).To(Equal(stringLit))
			Expect(val).To(Equal("it's"))
		})

		It("should unescape double quotes", func() {
			l := newLexer([]byte(`"say \"hello\""`))
			_, tok, val := l.Scan()
			Expect(tok).To(Equal(stringLit))
			Expect(val).To(Equal(`say "hello"`))
		})

		It("should not unescape mismatched quote type", func() {
			l := newLexer([]byte(`'contains \"double\"'`))
			_, tok, val := l.Scan()
			Expect(tok).To(Equal(stringLit))
			Expect(val).To(Equal(`contains \"double\"`))
		})
	})
})
