package store

import (
	"database/sql/driver"
	"fmt"

	duckdb "github.com/duckdb/duckdb-go/v2"
	duckdbmodels "github.com/kubev2v/migration-planner/pkg/duckdb_parser/models"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// rowScanner is satisfied by both *sql.Row and *sql.Rows, allowing scanXxx
// functions to work with both single-row and multi-row query results.
type rowScanner interface {
	Scan(dest ...any) error
}

type vmDetailDisk struct {
	duckdbmodels.Disk
	storageArrays      duckdbmodels.StorageArrays
	storageContainerID string
}

type vmDetailDisks []vmDetailDisk

func (d *vmDetailDisks) Scan(src any) error {
	if src == nil {
		*d = nil
		return nil
	}

	rows, ok := src.([]any)
	if !ok {
		return fmt.Errorf("scanning VM detail disks: expected []any, got %T", src)
	}

	var disks duckdbmodels.Disks
	err := disks.Scan(rows)
	if err != nil {
		return err
	}

	result := make(vmDetailDisks, 0, len(disks))
	// Planner's scanner preserves the order of the disk structs and skips non-struct entries.
	for _, row := range rows {
		fields, ok := row.(map[string]any)
		if !ok {
			continue
		}
		disk := vmDetailDisk{
			Disk:               disks[len(result)],
			storageContainerID: toString(fields["DatastoreStorageContainerID"]),
		}
		err = disk.storageArrays.Scan(fields["DatastoreStorageArrays"])
		if err != nil {
			return fmt.Errorf("scanning disk datastore storage arrays: %w", err)
		}
		result = append(result, disk)
	}
	*d = result
	return nil
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

// IntArray is a scanner for DuckDB INTEGER[] arrays.
type IntArray []int

func (a *IntArray) Scan(src any) error {
	if src == nil {
		*a = []int{}
		return nil
	}

	switch v := src.(type) {
	case []interface{}:
		result := make([]int, 0, len(v))

		for _, elem := range v {
			switch n := elem.(type) {
			case nil:
				continue
			case int:
				result = append(result, n)
			case int32:
				result = append(result, int(n))
			case int64:
				result = append(result, int(n))
			default:
				return fmt.Errorf("unsupported element for IntArray: %T", elem)
			}
		}

		*a = result
		return nil
	case []int32:
		result := make([]int, len(v))

		for i, elem := range v {
			result[i] = int(elem)
		}

		*a = result
		return nil
	case []int:
		*a = v
		return nil
	default:
		return fmt.Errorf("unsupported type for IntArray: %T", src)
	}
}

// BoolArray is a scanner for DuckDB BOOLEAN[] arrays.
type BoolArray []bool

func (a *BoolArray) Scan(src any) error {
	if src == nil {
		*a = []bool{}
		return nil
	}

	switch v := src.(type) {
	case []interface{}:
		result := make([]bool, 0, len(v))

		for _, elem := range v {
			switch b := elem.(type) {
			case nil:
				continue
			case bool:
				result = append(result, b)
			default:
				return fmt.Errorf("unsupported element for BoolArray: %T", elem)
			}
		}

		*a = result
		return nil
	case []bool:
		*a = v
		return nil
	default:
		return fmt.Errorf("unsupported type for BoolArray: %T", src)
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

type metadataMap map[string][]string

func (m *metadataMap) Scan(src any) error {
	var entries duckdb.OrderedMap
	err := entries.Scan(src)
	if err != nil {
		return err
	}

	keys, values := entries.Keys(), entries.Values()
	result := make(metadataMap, len(keys))
	for i, key := range keys {
		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("expected metadata key to be string, got %T", key)
		}

		var tags StringArray
		err = tags.Scan(values[i])
		if err != nil {
			return fmt.Errorf("scanning metadata values for %q: %w", name, err)
		}
		result[name] = tags
	}

	*m = result
	return nil
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
