package store

import (
	"context"
	"database/sql"
	"errors"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// Constants for vddk table
const (
	vddkTable      = "vddk"
	vddkColId      = "id"
	vddkColVersion = "version"
	vddkColMD5     = "md5"

	singleValidId = 1
)

type VddkStore struct {
	db QueryInterceptor
}

func NewVddkStore(db QueryInterceptor) *VddkStore {
	return &VddkStore{db: db}
}

func (s *VddkStore) Get(ctx context.Context) (*models.VddkStatus, error) {
	query, args, err := sq.Select(vddkColVersion, vddkColMD5).
		From(vddkTable).
		Where(sq.Eq{"id": singleValidId}).
		ToSql()
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	var status models.VddkStatus
	err = row.Scan(&status.Version, &status.Md5)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, srvErrors.NewVddkNotFoundError()
		}
		return nil, err
	}
	return &status, nil
}

func (s *VddkStore) Save(ctx context.Context, status *models.VddkStatus) error {
	query, args, err := sq.Insert(vddkTable).
		Columns(vddkColId, vddkColVersion, vddkColMD5).
		Values(singleValidId, status.Version, status.Md5).
		Suffix("ON CONFLICT (id) DO UPDATE SET version = EXCLUDED.version, md5 = EXCLUDED.md5").
		ToSql()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}
