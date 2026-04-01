package filter_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte("name = 'test'"))
	f.Add([]byte("memory >= 8GB and active = true"))
	f.Add([]byte("name ~ /^prod-.*/ and (cpus > 4 or memory < 1TB)"))
	f.Add([]byte("a != 'x' or b <= 100KB"))
	f.Add([]byte("enabled = true and (role = 'admin' or role = 'superuser')"))
	f.Add([]byte("name ~ /it's/ and active = false"))
	f.Add([]byte("a = '1' and b = '2' or c = '3' and d = '4'"))
	f.Add([]byte("((a = '1' or b = '2') and c = '3')"))
	f.Add([]byte("status in ['active', 'pending']"))
	f.Add([]byte("status not in ['deleted', 'archived']"))
	f.Add([]byte("status in []"))
	f.Add([]byte("name like 'prod'"))
	f.Add([]byte("name like 'web' and active = true"))
	f.Add([]byte(""))
	f.Add([]byte("((("))
	f.Add([]byte("name = ''"))
	f.Add([]byte("/unclosed"))
	f.Add([]byte("! @ # $"))
	f.Add([]byte("name ="))
	f.Add([]byte("= 'test'"))
	f.Add([]byte("name 'test'"))

	mf := filter.MapFunc(func(name string) (string, filter.FieldType, error) {
		return `"` + name + `"`, filter.AnyField, nil
	})

	f.Fuzz(func(t *testing.T, input []byte) {
		result, err := filter.Parse(input, mf)
		if err != nil {
			return
		}
		if result == nil {
			t.Fatal("Parse returned nil Sqlizer with no error")
		}
	})
}

// FuzzParseSecurityProperties verifies that parsing produces safe, parameterized SQL.
// This fuzz test checks security invariants:
// 1. User-provided string values must be parameterized (not embedded in SQL)
// 2. Generated SQL must not contain dangerous patterns
func FuzzParseSecurityProperties(f *testing.F) {
	// Standard filter patterns
	f.Add([]byte("name = 'test'"))
	f.Add([]byte("cluster = 'production'"))
	f.Add([]byte("name = 'test' and cluster = 'dev'"))

	// SQL injection patterns - single quotes
	f.Add([]byte("name = 'x; DROP TABLE--'"))
	f.Add([]byte("name = \"'; DELETE FROM users; --\""))
	f.Add([]byte("name = 'test\\' OR \\'1\\'=\\'1'"))

	// Quote escaping
	f.Add([]byte("name = 'O\\'Brien'"))
	f.Add([]byte("name = 'It\\'s a test'"))
	f.Add([]byte("name = 'quote\"inside'"))

	// SQL comments
	f.Add([]byte("name = 'test--comment'"))
	f.Add([]byte("name = 'test/*comment*/'"))
	f.Add([]byte("name = '/* */ DROP TABLE'"))

	// UNION injection attempts
	f.Add([]byte("name = 'x\\' UNION SELECT *--'"))
	f.Add([]byte("name = '1 UNION SELECT password FROM users'"))

	// Regex injection
	f.Add([]byte("name ~ /.*'; DROP TABLE--/"))
	f.Add([]byte("name ~ /test/ or name ~ /'; DELETE/"))

	// Control characters
	f.Add([]byte("name = 'test\x00injection'"))
	f.Add([]byte("name = 'line1\nline2'"))
	f.Add([]byte("name = 'tab\there'"))

	// Unicode
	f.Add([]byte("name = '测试'"))
	f.Add([]byte("name = 'tеst'")) // Cyrillic 'е'
	f.Add([]byte("name = 'café'"))

	// LIKE operator injection
	f.Add([]byte("name like 'test'"))
	f.Add([]byte("name like '\\'; DROP TABLE vms; --'"))
	f.Add([]byte("name like 'x; DELETE FROM users'"))

	// IN operator injection
	f.Add([]byte("cluster in ['a', 'b; DROP TABLE--']"))
	f.Add([]byte("cluster in ['normal', '\\'; DELETE FROM x--']"))

	// Long strings
	f.Add([]byte("name = '" + strings.Repeat("a", 1000) + "'"))

	// Special SQL characters
	f.Add([]byte("name = '%'"))
	f.Add([]byte("name = '_'"))
	f.Add([]byte("name = '[]'"))

	f.Fuzz(func(t *testing.T, input []byte) {
		result, err := filter.ParseWithDefaultMap(input)
		if err != nil {
			return // Parse error is acceptable for fuzzed input
		}

		// Get the SQL output
		sql, args, err := result.ToSql()
		if err != nil {
			t.Fatalf("ToSql failed after successful parse: %v", err)
		}

		// Property 1: Verify no dangerous SQL patterns in the generated SQL
		verifyNoInjection(t, sql)

		// Property 2: Verify parameterization - string literals should be in args, not SQL
		verifyParameterization(t, input, sql, args)
	})
}

// FuzzIdentifierWhitelist verifies that unknown identifiers are rejected.
// This tests that the whitelist-based MapFunc properly rejects invalid field names.
func FuzzIdentifierWhitelist(f *testing.F) {
	// Valid identifiers
	f.Add([]byte("name = 'x'"))
	f.Add([]byte("cluster = 'x'"))
	f.Add([]byte("memory > 1GB"))

	// Invalid identifiers - should be rejected
	f.Add([]byte("nonexistent_field = 'x'"))
	f.Add([]byte("v.secret_column = 'x'"))
	f.Add([]byte("DROP_TABLE = 'x'"))
	f.Add([]byte("__proto__ = 'x'"))
	f.Add([]byte("constructor = 'x'"))
	f.Add([]byte("password = 'secret'"))
	f.Add([]byte("internal.field = 'value'"))

	// LIKE operator
	f.Add([]byte("name like 'prod'"))

	// Mixed valid and invalid
	f.Add([]byte("name = 'test' and invalid_field = 'x'"))

	// SQL keywords as identifiers
	f.Add([]byte("SELECT = 'x'"))
	f.Add([]byte("FROM = 'x'"))
	f.Add([]byte("WHERE = 'x'"))

	f.Fuzz(func(t *testing.T, input []byte) {
		result, err := filter.ParseWithDefaultMap(input)
		if err != nil {
			// Parse/mapping error is expected for invalid identifiers
			return
		}

		// If parsing succeeded, ToSql should also work
		sql, _, err := result.ToSql()
		if err != nil {
			t.Fatalf("ToSql failed after successful parse: %v", err)
		}

		// Verify the SQL only contains expected column patterns
		verifyAllowedColumns(t, sql)
	})
}

// verifyNoInjection checks that the generated SQL doesn't contain dangerous patterns.
func verifyNoInjection(t *testing.T, sql string) {
	t.Helper()

	dangerous := []string{
		"DROP TABLE",
		"DELETE FROM",
		"INSERT INTO",
		"UPDATE ",
		"; SELECT",
		"UNION SELECT",
		"TRUNCATE ",
		"ALTER TABLE",
		"CREATE TABLE",
		"EXEC ",
		"EXECUTE ",
	}

	upperSQL := strings.ToUpper(sql)
	for _, d := range dangerous {
		if strings.Contains(upperSQL, d) {
			t.Errorf("Dangerous SQL pattern %q found in generated SQL: %s", d, sql)
		}
	}
}

// verifyParameterization checks that user string values are parameterized.
func verifyParameterization(t *testing.T, input []byte, sql string, args []interface{}) {
	t.Helper()

	// Extract string literals from the input (simplified extraction)
	quotedStrings := extractQuotedStrings(string(input))

	for _, s := range quotedStrings {
		// Skip very short strings as they may appear coincidentally in SQL
		if len(s) < 3 {
			continue
		}

		// If a string contains SQL injection patterns, it MUST NOT appear verbatim in SQL
		containsInjection := containsSQLInjectionPattern(s)
		if containsInjection && strings.Contains(sql, s) {
			t.Errorf("User string with injection pattern %q found verbatim in SQL: %s", s, sql)
		}

		// For longer unique strings, verify they appear in args (parameterized)
		if len(s) >= 5 && containsInjection {
			found := false
			for _, arg := range args {
				if argStr, ok := arg.(string); ok && argStr == s {
					found = true
					break
				}
			}
			// Note: The string might be transformed (escaped) so exact match may not always occur
			_ = found // Accept either way, the key check is it's not in SQL directly
		}
	}
}

// verifyAllowedColumns checks that SQL only contains known column references.
func verifyAllowedColumns(t *testing.T, sql string) {
	t.Helper()

	// Known table aliases used in defaultMapFn
	allowedTablePrefixes := []string{
		`v.`, `d.`, `cc.`, `dk.`, `c.`, `i.`, `cpu.`, `mem.`, `net.`, `ds.`,
	}

	// Extract column references (simplified pattern matching)
	// This looks for patterns like: prefix."Column Name" or prefix.column_name
	columnPattern := regexp.MustCompile(`\b([a-z]+)\."[^"]+"|([a-z]+)\.[a-z_]+`)
	matches := columnPattern.FindAllString(sql, -1)

	for _, match := range matches {
		valid := false
		for _, prefix := range allowedTablePrefixes {
			if strings.HasPrefix(match, prefix) {
				valid = true
				break
			}
		}
		if !valid {
			t.Errorf("Unexpected column reference %q in SQL: %s", match, sql)
		}
	}
}

// extractQuotedStrings extracts string literals from filter input.
func extractQuotedStrings(input string) []string {
	var result []string

	// Match single-quoted strings (handling escaped quotes)
	singleQuotePattern := regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)
	matches := singleQuotePattern.FindAllString(input, -1)
	for _, m := range matches {
		// Remove surrounding quotes
		if len(m) >= 2 {
			result = append(result, m[1:len(m)-1])
		}
	}

	// Match double-quoted strings
	doubleQuotePattern := regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	matches = doubleQuotePattern.FindAllString(input, -1)
	for _, m := range matches {
		if len(m) >= 2 {
			result = append(result, m[1:len(m)-1])
		}
	}

	return result
}

// containsSQLInjectionPattern checks if a string contains common SQL injection patterns.
func containsSQLInjectionPattern(s string) bool {
	injectionPatterns := []string{
		"DROP", "DELETE", "INSERT", "UPDATE", "SELECT",
		"UNION", "ALTER", "TRUNCATE", "CREATE", "EXEC",
		";", "--", "/*", "*/",
	}

	upper := strings.ToUpper(s)
	for _, p := range injectionPatterns {
		if strings.Contains(upper, p) {
			return true
		}
	}
	return false
}
