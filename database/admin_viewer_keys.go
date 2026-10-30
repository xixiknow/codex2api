package database

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// 只读管理密钥（viewer key）：持有者可登录管理后台查看仪表盘/账号池/用量，
// 但不能执行任何写操作，也拿不到令牌等秘密。表中只保存 SHA-256 哈希，明文仅在创建时返回一次。
const adminViewerKeysPostgresSchema = `
	CREATE TABLE IF NOT EXISTS admin_viewer_keys (
		id           SERIAL PRIMARY KEY,
		name         VARCHAR(255) NOT NULL DEFAULT '',
		key_prefix   VARCHAR(32) NOT NULL DEFAULT '',
		key_hash     VARCHAR(128) NOT NULL UNIQUE,
		created_at   TIMESTAMPTZ DEFAULT NOW(),
		last_used_at TIMESTAMPTZ NULL,
		revoked_at   TIMESTAMPTZ NULL
	)`

const adminViewerKeysSQLiteSchema = `CREATE TABLE IF NOT EXISTS admin_viewer_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL DEFAULT '',
		key_prefix TEXT NOT NULL DEFAULT '',
		key_hash TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_used_at TIMESTAMP NULL,
		revoked_at TIMESTAMP NULL
	);`

// AdminViewerKey 只读管理密钥记录（不含明文）。
type AdminViewerKey struct {
	ID         int64
	Name       string
	KeyPrefix  string
	KeyHash    string
	CreatedAt  time.Time
	LastUsedAt sql.NullTime
	RevokedAt  sql.NullTime
}

const adminViewerKeyColumns = `id, name, key_prefix, key_hash, created_at, last_used_at, revoked_at`

func scanAdminViewerKey(scan func(dest ...interface{}) error) (*AdminViewerKey, error) {
	var k AdminViewerKey
	var createdRaw, lastUsedRaw, revokedRaw interface{}
	if err := scan(&k.ID, &k.Name, &k.KeyPrefix, &k.KeyHash, &createdRaw, &lastUsedRaw, &revokedRaw); err != nil {
		return nil, err
	}
	var err error
	if k.CreatedAt, err = parseDBTimeValue(createdRaw); err != nil {
		return nil, err
	}
	if k.LastUsedAt, err = parseDBNullTimeValue(lastUsedRaw); err != nil {
		return nil, err
	}
	if k.RevokedAt, err = parseDBNullTimeValue(revokedRaw); err != nil {
		return nil, err
	}
	return &k, nil
}

// CreateAdminViewerKey 插入一条只读密钥并返回 ID。
func (db *DB) CreateAdminViewerKey(ctx context.Context, name, keyPrefix, keyHash string) (int64, error) {
	name = strings.TrimSpace(name)
	return db.insertRowID(ctx,
		`INSERT INTO admin_viewer_keys (name, key_prefix, key_hash, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		`INSERT INTO admin_viewer_keys (name, key_prefix, key_hash, created_at) VALUES ($1, $2, $3, $4)`,
		name, keyPrefix, keyHash, time.Now().UTC())
}

// ListAdminViewerKeys 按创建时间倒序列出所有只读密钥（含已吊销）。
func (db *DB) ListAdminViewerKeys(ctx context.Context) ([]AdminViewerKey, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT `+adminViewerKeyColumns+` FROM admin_viewer_keys ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]AdminViewerKey, 0)
	for rows.Next() {
		k, err := scanAdminViewerKey(rows.Scan)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *k)
	}
	return keys, rows.Err()
}

// RevokeAdminViewerKey 吊销密钥并返回其哈希（供调用方清理缓存）；不存在时返回 sql.ErrNoRows。
func (db *DB) RevokeAdminViewerKey(ctx context.Context, id int64) (string, error) {
	var hash string
	if err := db.conn.QueryRowContext(ctx, `SELECT key_hash FROM admin_viewer_keys WHERE id = $1`, id).Scan(&hash); err != nil {
		return "", err
	}
	if _, err := db.conn.ExecContext(ctx,
		`UPDATE admin_viewer_keys SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`,
		time.Now().UTC(), id); err != nil {
		return "", err
	}
	return hash, nil
}

// FindAdminViewerKeyByHash 按哈希查找未吊销的密钥；找不到时返回 (nil, nil)。
func (db *DB) FindAdminViewerKeyByHash(ctx context.Context, keyHash string) (*AdminViewerKey, error) {
	row := db.conn.QueryRowContext(ctx,
		`SELECT `+adminViewerKeyColumns+` FROM admin_viewer_keys WHERE key_hash = $1 AND revoked_at IS NULL`, keyHash)
	k, err := scanAdminViewerKey(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

// TouchAdminViewerKey 更新最近使用时间。
func (db *DB) TouchAdminViewerKey(ctx context.Context, id int64, at time.Time) error {
	_, err := db.conn.ExecContext(ctx, `UPDATE admin_viewer_keys SET last_used_at = $1 WHERE id = $2`, at.UTC(), id)
	return err
}
