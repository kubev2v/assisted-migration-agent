package filter_test

import (
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
	f.Add([]byte(""))
	f.Add([]byte("((("))
	f.Add([]byte("name = ''"))
	f.Add([]byte("/unclosed"))
	f.Add([]byte("! @ # $"))
	f.Add([]byte("name ="))
	f.Add([]byte("= 'test'"))
	f.Add([]byte("name 'test'"))

	mf := filter.MapFunc(func(name string) (string, error) {
		return `"` + name + `"`, nil
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
