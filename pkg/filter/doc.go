package filter

// Grammar
//
// --- PARSER RULES ---

// expression  : term ( "or" term )* ;
// term        : factor ( "and" factor )* ;
//
// factor      : equality
//             | "(" expression ")" ;
//
// // Regex gets its own distinct rule based on the operator used
// equality    : IDENTIFIER ( "=" | "!=" | "<" | "<=" | ">" | ">=" ) value
//             | IDENTIFIER ( "~" | "!~" ) REGEX_LITERAL ;
//
// value       : STRING | QUANTITY | BOOLEAN ;
//
// // --- LEXER RULES ---
//
// IDENTIFIER    : [a-zA-Z_.]+ ;
//
// // AWK-style regex: /pattern/
// // Matches anything between two forward slashes
// REGEX_LITERAL : '/' ( '\\/' | . )*? '/' ;
//
// STRING        : "'" (.*?) "'" | "\"" (.*?) "\"" ;
// BOOLEAN       : "true" | "false" ;
//
// // Numeric value with optional unit suffix
// QUANTITY      : [0-9]+(\.[0-9]+)? ( 'KB' | 'MB' | 'GB' | 'TB' | 'kb' | 'mb' | 'gb' | 'tb' )? ;

// This should be the query agains which the filter is applied
//
// {{- /*
// Flattened VM Query — joins all tables without aggregation so every column
// is directly available in the WHERE clause for filtering.
//
// A VM with N disks and M NICs produces N×M rows (cartesian product).
// Use SELECT DISTINCT i."VM ID" when you only need matching VM IDs.
//
// Template Parameters:
//   - Filter: raw SQL WHERE expression from the filter parser (optional)
//   - Limit:  max results, 0 = unlimited (optional)
//   - Offset: skip first N results (optional)
// */ -}}
// SELECT DISTINCT i."VM ID" AS id
// FROM vinfo i
// LEFT JOIN vcpu c ON i."VM ID" = c."VM ID"
// LEFT JOIN vmemory m ON i."VM ID" = m."VM ID"
// LEFT JOIN vdisk dk ON i."VM ID" = dk."VM ID"
// LEFT JOIN vdatastore ds ON ds."Name" = regexp_extract(COALESCE(dk."Path", dk."Disk Path"), '\[([^\]]+)\]', 1)
// LEFT JOIN vnetwork n ON i."VM ID" = n."VM ID"
// LEFT JOIN concerns con ON i."VM ID" = con."VM_ID"
// WHERE 1=1
// {{- if .Filter }} AND {{ .Filter }}{{ end }}
// {{- if and .Limit (gt .Limit 0) }} LIMIT {{ .Limit }}{{ end }}
// {{- if and .Offset (gt .Offset 0) }} OFFSET {{ .Offset }}{{ end }};
//
