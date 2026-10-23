package store

import (
	"database/sql/driver"
	"fmt"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// rowScanner is satisfied by both *sql.Row and *sql.Rows, allowing scanXxx
// functions to work with both single-row and multi-row query results.
type rowScanner interface {
	Scan(dest ...any) error
}

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

type AppProducts []models.AppProduct

func (n *AppProducts) Scan(value interface{}) error {
	if value == nil {
		*n = nil
		return nil
	}
	slice, ok := value.([]interface{})
	if !ok {
		return fmt.Errorf("NICs.Scan: expected []interface{}, got %T", value)
	}
	result := make([]models.AppProduct, 0, len(slice))
	for _, item := range slice {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		p := models.AppProduct{
			Key:         m["key"].(int32),
			ClassId:     toString(m["classId"]),
			InstanceId:  toString(m["instanceId"]),
			Name:        toString(m["name"]),
			Vendor:      toString(m["vendor"]),
			Version:     toString(m["version"]),
			FullVersion: toString(m["fullVersion"]),
			VendorUrl:   toString(m["vendorUrl"]),
			ProductUrl:  toString(m["productUrl"]),
			AppUrl:      toString(m["appUrl"]),
		}
		result = append(result, p)
	}
	*n = result
	return nil
}

func (n AppProducts) Value() (driver.Value, error) {
	return n, nil
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
