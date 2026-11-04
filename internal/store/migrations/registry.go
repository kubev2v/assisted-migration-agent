package migrations

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

type migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

type registry []migration

var migrationName = regexp.MustCompile(`^([0-9]{3,})_([a-z0-9_]+)\.sql$`)

func loadRegistry(files fs.FS, dir string) (registry, error) {
	versions := make(map[int]*migration)
	for _, direction := range []string{"up", "down"} {
		entries, err := fs.ReadDir(files, path.Join(dir, direction))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				return nil, fmt.Errorf("unexpected migration entry: %s", entry.Name())
			}
			parts := migrationName.FindStringSubmatch(entry.Name())
			if parts == nil {
				return nil, fmt.Errorf("invalid migration filename: %s", entry.Name())
			}
			version, err := strconv.Atoi(parts[1])
			if err != nil || version < 1 || parts[1] != fmt.Sprintf("%03d", version) {
				return nil, fmt.Errorf("invalid migration version: %s", entry.Name())
			}
			m := versions[version]
			if m == nil {
				m = &migration{Version: version, Name: parts[2]}
				versions[version] = m
			}
			if m.Name != parts[2] {
				return nil, fmt.Errorf("conflicting migration names for version %d", version)
			}
			content, err := fs.ReadFile(files, path.Join(dir, direction, entry.Name()))
			if err != nil {
				return nil, err
			}
			var statements []string
			for _, line := range strings.Split(string(content), "\n") {
				statements = append(statements, strings.SplitN(line, "--", 2)[0])
			}
			if strings.TrimSpace(strings.Join(statements, "\n")) == "" {
				return nil, fmt.Errorf("empty migration: %s", entry.Name())
			}
			if direction == "down" {
				if m.Down != "" {
					return nil, fmt.Errorf("duplicate down migration: %d", version)
				}
				if _, err := template.New(entry.Name()).Parse(string(content)); err != nil {
					return nil, fmt.Errorf("invalid down migration template: %w", err)
				}
				m.Down = string(content)
			} else {
				if m.Up != "" {
					return nil, fmt.Errorf("duplicate up migration: %d", version)
				}
				m.Up = string(content)
			}
		}
	}

	result := make(registry, 0, len(versions))
	for _, m := range versions {
		result = append(result, *m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	for i, m := range result {
		if m.Version != i+1 || m.Up == "" || m.Down == "" {
			return nil, fmt.Errorf("migration %d requires contiguous versions and an up/down pair", m.Version)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no migrations in %s", dir)
	}
	return result, nil
}

func (r registry) checksum() string {
	h := sha256.New()
	for _, m := range r {
		_, _ = fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00", m.Version, m.Name, m.Up, m.Down)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (r registry) validateHistory(versions []int) error {
	for i, version := range versions {
		if version != i+1 || version > len(r) {
			return fmt.Errorf("unsupported migration history: %v", versions)
		}
	}
	return nil
}
