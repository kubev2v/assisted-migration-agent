package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type Source struct {
	ID        string
	Name      string
	OrgID     string
	CreatedAt time.Time
}

type Assessment struct {
	ID         string
	Name       string
	SourceID   *string
	OrgID      string
	SourceType string
	Snapshot   *Snapshot
}

type Snapshot struct {
	ID           int
	AssessmentID string
	Inventory    []byte
	CreatedAt    time.Time
}

type Agent struct {
	ID       string
	Status   string
	SourceID string
}

type DbReadWriter struct {
	db   *sql.DB
	psql sq.StatementBuilderType
}

func NewDbReadWriter(connString string) (*DbReadWriter, error) {
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &DbReadWriter{
		db:   db,
		psql: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}, nil
}

func (d *DbReadWriter) Close() error {
	return d.db.Close()
}

func (d *DbReadWriter) ListSources(ctx context.Context) ([]Source, error) {
	query, args, err := d.psql.Select("id", "name", "org_id", "created_at").
		From("sources").
		Where(sq.Eq{"deleted_at": nil}).
		ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.ID, &s.Name, &s.OrgID, &s.CreatedAt); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

func (d *DbReadWriter) ListAgents(ctx context.Context) ([]Agent, error) {
	query, args, err := d.psql.Select("id", "status", "source_id").
		From("agents").
		Where(sq.Eq{"deleted_at": nil}).
		ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Status, &a.SourceID); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (d *DbReadWriter) ListAssessments(ctx context.Context) ([]Assessment, error) {
	query, args, err := d.psql.Select(
		"a.id", "a.name", "a.source_id", "a.org_id", "a.source_type",
		"s.id", "s.inventory", "s.created_at",
	).
		From("assessments a").
		LeftJoin("snapshots s ON s.assessment_id = a.id").
		ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assessments []Assessment
	for rows.Next() {
		var a Assessment
		var snapshotID sql.NullInt64
		var snapshotInventory []byte
		var snapshotCreatedAt sql.NullTime

		if err := rows.Scan(
			&a.ID, &a.Name, &a.SourceID, &a.OrgID, &a.SourceType,
			&snapshotID, &snapshotInventory, &snapshotCreatedAt,
		); err != nil {
			return nil, err
		}

		if snapshotID.Valid {
			a.Snapshot = &Snapshot{
				ID:           int(snapshotID.Int64),
				AssessmentID: a.ID,
				Inventory:    snapshotInventory,
				CreatedAt:    snapshotCreatedAt.Time,
			}
		}
		assessments = append(assessments, a)
	}
	return assessments, rows.Err()
}

type Key struct {
	ID         string
	OrgID      string
	PrivateKey *rsa.PrivateKey
}

func (d *DbReadWriter) GetPrivateKey(ctx context.Context, orgID string) (*Key, error) {
	query, args, err := d.psql.Select("id", "org_id", "private_key").
		From("keys").
		Where(sq.Eq{"org_id": orgID}).
		ToSql()
	if err != nil {
		return nil, err
	}

	var key Key
	var pemData string
	if err := d.db.QueryRowContext(ctx, query, args...).Scan(&key.ID, &key.OrgID, &pemData); err != nil {
		return nil, err
	}

	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key.PrivateKey = privateKey

	return &key, nil
}

func (d *DbReadWriter) CreatePrivateKey(ctx context.Context, orgID string) (string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}

	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	id := uuid.NewString()
	now := time.Now()

	query, args, err := d.psql.Insert("keys").
		Columns("id", "org_id", "private_key", "created_at", "updated_at").
		Values(id, orgID, string(pemData), now, now).
		ToSql()
	if err != nil {
		return "", err
	}

	_, err = d.db.ExecContext(ctx, query, args...)
	if err != nil {
		return "", err
	}

	return id, nil
}
