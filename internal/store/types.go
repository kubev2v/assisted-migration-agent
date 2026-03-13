package store

import "fmt"

// StringArray is a scanner for DuckDB VARCHAR[] arrays.
// DuckDB returns arrays as []interface{}, this converts them to []string.
type StringArray []string

// Scan implements sql.Scanner for DuckDB VARCHAR[] arrays.
func (s *StringArray) Scan(src any) error {
	if src == nil {
		*s = []string{}
		return nil
	}

	switch v := src.(type) {
	case []interface{}:
		result := make([]string, len(v))
		for i, elem := range v {
			if elem == nil {
				result[i] = ""
			} else if str, ok := elem.(string); ok {
				result[i] = str
			} else {
				result[i] = fmt.Sprintf("%v", elem)
			}
		}
		*s = result
		return nil
	case []string:
		*s = v
		return nil
	default:
		return fmt.Errorf("unsupported type for StringArray: %T", src)
	}
}
