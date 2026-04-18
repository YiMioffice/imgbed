package persist

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"machring/internal/auth"
	"machring/internal/policy"
	"machring/internal/resource"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLite(path string, defaultRules []policy.Rule) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	configureSQLite(db)

	store := &SQLiteStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.seedUserGroups(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.seedPolicyGroups(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.seedRules(context.Background(), defaultRules); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.seedSiteSettings(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.importLegacyJSON(context.Background(), path); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func configureSQLite(db *sql.DB) {
	connections := runtime.GOMAXPROCS(0)
	if connections < 4 {
		connections = 4
	}
	if connections > 16 {
		connections = 16
	}
	db.SetMaxOpenConns(connections)
	db.SetMaxIdleConns(connections)
	db.SetConnMaxLifetime(30 * time.Minute)
}

func (s *SQLiteStore) Rules(ctx context.Context) ([]policy.Rule, error) {
	group, err := s.ActivePolicyGroup(ctx)
	if err != nil {
		return nil, err
	}
	return s.RulesForGroup(ctx, group.ID)
}

func (s *SQLiteStore) RulesForGroup(ctx context.Context, groupID string) ([]policy.Rule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_group, resource_type, extension, allow_upload, allow_access,
			max_file_size_bytes, monthly_traffic_per_resource_bytes,
			monthly_traffic_per_user_and_type_bytes, require_auth, require_review,
			force_private, cache_control, download_disposition
		FROM policy_rules
		WHERE policy_group_id = ?
		ORDER BY position, id
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []policy.Rule
	for rows.Next() {
		var rule policy.Rule
		var allowUpload, allowAccess, requireAuth, requireReview, forcePrivate boolInt
		if err := rows.Scan(
			&rule.UserGroup,
			&rule.ResourceType,
			&rule.Extension,
			&allowUpload,
			&allowAccess,
			&rule.MaxFileSizeBytes,
			&rule.MonthlyTrafficPerResourceBytes,
			&rule.MonthlyTrafficPerUserAndTypeBytes,
			&requireAuth,
			&requireReview,
			&forcePrivate,
			&rule.CacheControl,
			&rule.DownloadDisposition,
		); err != nil {
			return nil, err
		}
		rule.AllowUpload = bool(allowUpload)
		rule.AllowAccess = bool(allowAccess)
		rule.RequireAuth = bool(requireAuth)
		rule.RequireReview = bool(requireReview)
		rule.ForcePrivate = bool(forcePrivate)
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *SQLiteStore) ReplaceRules(ctx context.Context, rules []policy.Rule) error {
	group, err := s.ActivePolicyGroup(ctx)
	if err != nil {
		return err
	}
	return s.ReplaceRulesForGroup(ctx, group.ID, rules)
}

func (s *SQLiteStore) ReplaceRulesForGroup(ctx context.Context, groupID string, rules []policy.Rule) error {
	if _, _, err := s.PolicyGroup(ctx, groupID); err != nil {
		return err
	}
	return s.replaceRulesForGroup(ctx, groupID, rules)
}

func (s *SQLiteStore) replaceRulesForGroup(ctx context.Context, groupID string, rules []policy.Rule) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_rules WHERE policy_group_id = ?`, groupID); err != nil {
		return err
	}
	for i, rule := range rules {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO policy_rules (
				policy_group_id, position, user_group, resource_type, extension, allow_upload, allow_access,
				max_file_size_bytes, monthly_traffic_per_resource_bytes,
				monthly_traffic_per_user_and_type_bytes, require_auth, require_review,
				force_private, cache_control, download_disposition
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			groupID,
			i,
			rule.UserGroup,
			string(rule.ResourceType),
			rule.Extension,
			boolInt(rule.AllowUpload),
			boolInt(rule.AllowAccess),
			rule.MaxFileSizeBytes,
			rule.MonthlyTrafficPerResourceBytes,
			rule.MonthlyTrafficPerUserAndTypeBytes,
			boolInt(rule.RequireAuth),
			boolInt(rule.RequireReview),
			boolInt(rule.ForcePrivate),
			rule.CacheControl,
			rule.DownloadDisposition,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ActivePolicyGroup(ctx context.Context) (policy.Group, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, is_active, is_default, created_at, updated_at
		FROM policy_groups
		WHERE is_active = 1
		ORDER BY is_default DESC, created_at ASC
		LIMIT 1
	`)
	group, err := scanPolicyGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return policy.Group{}, policy.ErrPolicyGroupNotFound
	}
	return group, err
}

func (s *SQLiteStore) PolicyGroups(ctx context.Context) ([]policy.Group, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, is_active, is_default, created_at, updated_at
		FROM policy_groups
		ORDER BY is_default DESC, created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []policy.Group
	for rows.Next() {
		group, err := scanPolicyGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *SQLiteStore) PolicyGroup(ctx context.Context, groupID string) (policy.Group, []policy.Rule, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, is_active, is_default, created_at, updated_at
		FROM policy_groups
		WHERE id = ?
	`, groupID)
	group, err := scanPolicyGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return policy.Group{}, nil, policy.ErrPolicyGroupNotFound
	}
	if err != nil {
		return policy.Group{}, nil, err
	}
	rules, err := s.RulesForGroup(ctx, groupID)
	return group, rules, err
}

func (s *SQLiteStore) CreatePolicyGroup(ctx context.Context, name, description string) (policy.Group, error) {
	now := time.Now()
	groupID, err := newID("polgrp")
	if err != nil {
		return policy.Group{}, err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO policy_groups (id, name, description, is_active, is_default, created_at, updated_at)
		VALUES (?, ?, ?, 0, 0, ?, ?)
	`, groupID, name, description, formatTime(now), formatTime(now)); err != nil {
		return policy.Group{}, err
	}
	group, _, err := s.PolicyGroup(ctx, groupID)
	return group, err
}

func (s *SQLiteStore) UpdatePolicyGroup(ctx context.Context, groupID, name, description string) (policy.Group, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE policy_groups
		SET name = ?, description = ?, updated_at = ?
		WHERE id = ?
	`, name, description, formatTime(time.Now()), groupID)
	if err != nil {
		return policy.Group{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		return policy.Group{}, policy.ErrPolicyGroupNotFound
	}
	group, _, err := s.PolicyGroup(ctx, groupID)
	return group, err
}

func (s *SQLiteStore) DeletePolicyGroup(ctx context.Context, groupID string) error {
	group, _, err := s.PolicyGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if group.IsDefault || group.IsActive {
		return policy.ErrPolicyGroupInUse
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM policy_rules WHERE policy_group_id = ?`, groupID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM policy_groups WHERE id = ?`, groupID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		return policy.ErrPolicyGroupNotFound
	}
	return tx.Commit()
}

func (s *SQLiteStore) CopyPolicyGroup(ctx context.Context, sourceGroupID, name string) (policy.Group, error) {
	sourceGroup, rules, err := s.PolicyGroup(ctx, sourceGroupID)
	if err != nil {
		return policy.Group{}, err
	}
	newName := strings.TrimSpace(name)
	if newName == "" {
		newName = sourceGroup.Name + " 副本"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return policy.Group{}, err
	}
	defer tx.Rollback()

	now := time.Now()
	groupID, err := newID("polgrp")
	if err != nil {
		return policy.Group{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO policy_groups (id, name, description, is_active, is_default, created_at, updated_at)
		VALUES (?, ?, ?, 0, 0, ?, ?)
	`, groupID, newName, sourceGroup.Description, formatTime(now), formatTime(now)); err != nil {
		return policy.Group{}, err
	}
	for i, rule := range rules {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO policy_rules (
				policy_group_id, position, user_group, resource_type, extension, allow_upload, allow_access,
				max_file_size_bytes, monthly_traffic_per_resource_bytes,
				monthly_traffic_per_user_and_type_bytes, require_auth, require_review,
				force_private, cache_control, download_disposition
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			groupID,
			i,
			rule.UserGroup,
			string(rule.ResourceType),
			rule.Extension,
			boolInt(rule.AllowUpload),
			boolInt(rule.AllowAccess),
			rule.MaxFileSizeBytes,
			rule.MonthlyTrafficPerResourceBytes,
			rule.MonthlyTrafficPerUserAndTypeBytes,
			boolInt(rule.RequireAuth),
			boolInt(rule.RequireReview),
			boolInt(rule.ForcePrivate),
			rule.CacheControl,
			rule.DownloadDisposition,
		); err != nil {
			return policy.Group{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return policy.Group{}, err
	}
	group, _, err := s.PolicyGroup(ctx, groupID)
	return group, err
}

func (s *SQLiteStore) SetPolicyGroupActive(ctx context.Context, groupID string, active bool) (policy.Group, error) {
	group, _, err := s.PolicyGroup(ctx, groupID)
	if err != nil {
		return policy.Group{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return policy.Group{}, err
	}
	defer tx.Rollback()

	now := formatTime(time.Now())
	if active {
		if _, err := tx.ExecContext(ctx, `UPDATE policy_groups SET is_active = 0, updated_at = ?`, now); err != nil {
			return policy.Group{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE policy_groups SET is_active = 1, updated_at = ? WHERE id = ?`, now, groupID); err != nil {
			return policy.Group{}, err
		}
	} else {
		if group.IsActive {
			var activeCount int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM policy_groups WHERE is_active = 1`).Scan(&activeCount); err != nil {
				return policy.Group{}, err
			}
			if activeCount <= 1 {
				return policy.Group{}, policy.ErrPolicyGroupInvalidState
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE policy_groups SET is_active = 0, updated_at = ? WHERE id = ?`, now, groupID); err != nil {
			return policy.Group{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return policy.Group{}, err
	}
	group, _, err = s.PolicyGroup(ctx, groupID)
	return group, err
}

func (s *SQLiteStore) InstallState(ctx context.Context) (InstallState, error) {
	state := InstallState{
		SiteName:       "马赫环",
		DefaultStorage: "local",
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT u.username
		FROM users u
		WHERE u.role = ?
		ORDER BY u.created_at ASC
		LIMIT 1
	`, auth.AdminRole)
	switch err := row.Scan(&state.AdminUsername); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return InstallState{}, err
	default:
		state.Initialized = true
	}

	settingsRows, err := s.db.QueryContext(ctx, `
		SELECT key, value
		FROM app_settings
		WHERE key IN ('site_name', 'default_storage')
	`)
	if err != nil {
		return InstallState{}, err
	}
	defer settingsRows.Close()

	for settingsRows.Next() {
		var key string
		var value string
		if err := settingsRows.Scan(&key, &value); err != nil {
			return InstallState{}, err
		}
		switch key {
		case "site_name":
			if value != "" {
				state.SiteName = value
			}
		case "default_storage":
			if value != "" {
				state.DefaultStorage = value
			}
		}
	}
	if err := settingsRows.Err(); err != nil {
		return InstallState{}, err
	}

	return state, nil
}

func (s *SQLiteStore) Initialize(ctx context.Context, params InitializeParams) (auth.User, error) {
	state, err := s.InstallState(ctx)
	if err != nil {
		return auth.User{}, err
	}
	if state.Initialized {
		return auth.User{}, ErrAlreadyInitialized
	}

	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.User{}, err
	}
	defer tx.Rollback()

	if err := s.seedUserGroupsTx(ctx, tx, now); err != nil {
		return auth.User{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES ('site_name', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, params.SiteName, formatTime(now)); err != nil {
		return auth.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES ('default_storage', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, params.DefaultStorage, formatTime(now)); err != nil {
		return auth.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO site_settings (
			id, site_name, external_base_url, allow_guest_uploads, show_stats_on_home, show_featured_on_home, updated_at
		) VALUES (1, ?, '', 1, 1, 1, ?)
		ON CONFLICT(id) DO UPDATE SET
			site_name = excluded.site_name,
			updated_at = excluded.updated_at
	`, params.SiteName, formatTime(now)); err != nil {
		return auth.User{}, err
	}

	userID, err := newID("usr")
	if err != nil {
		return auth.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (
			id, username, display_name, password_hash, role, user_group_id, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		userID,
		params.AdminUsername,
		params.DisplayName,
		params.PasswordHash,
		auth.AdminRole,
		policy.GroupAdmin,
		"active",
		formatTime(now),
		formatTime(now),
	); err != nil {
		return auth.User{}, err
	}

	if err := tx.Commit(); err != nil {
		return auth.User{}, err
	}

	return s.UserByID(ctx, userID)
}

func (s *SQLiteStore) UserByUsername(ctx context.Context, username string) (auth.User, string, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.role, u.user_group_id,
			COALESCE(g.name, u.user_group_id), u.status, u.password_hash
		FROM users u
		LEFT JOIN user_groups g ON g.id = u.user_group_id
		WHERE u.username = ?
	`, username)

	var user auth.User
	var passwordHash string
	if err := row.Scan(
		&user.ID,
		&user.Username,
		&user.DisplayName,
		&user.Role,
		&user.GroupID,
		&user.GroupName,
		&user.Status,
		&passwordHash,
	); errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, "", auth.ErrUserNotFound
	} else if err != nil {
		return auth.User{}, "", err
	}

	return user, passwordHash, nil
}

func (s *SQLiteStore) UserByID(ctx context.Context, id string) (auth.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.role, u.user_group_id,
			COALESCE(g.name, u.user_group_id), u.status
		FROM users u
		LEFT JOIN user_groups g ON g.id = u.user_group_id
		WHERE u.id = ?
	`, id)

	var user auth.User
	if err := row.Scan(
		&user.ID,
		&user.Username,
		&user.DisplayName,
		&user.Role,
		&user.GroupID,
		&user.GroupName,
		&user.Status,
	); errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	} else if err != nil {
		return auth.User{}, err
	}

	return user, nil
}

func (s *SQLiteStore) UserGroups(ctx context.Context) ([]UserGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, total_capacity_bytes, default_monthly_traffic_bytes,
			max_file_size_bytes, daily_upload_limit, allow_hotlink, created_at, updated_at
		FROM user_groups
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []UserGroup
	for rows.Next() {
		group, err := scanUserGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *SQLiteStore) UpdateUserGroup(ctx context.Context, group UserGroup) (UserGroup, error) {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_groups (
			id, name, description, total_capacity_bytes, default_monthly_traffic_bytes,
			max_file_size_bytes, daily_upload_limit, allow_hotlink, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			total_capacity_bytes = excluded.total_capacity_bytes,
			default_monthly_traffic_bytes = excluded.default_monthly_traffic_bytes,
			max_file_size_bytes = excluded.max_file_size_bytes,
			daily_upload_limit = excluded.daily_upload_limit,
			allow_hotlink = excluded.allow_hotlink,
			updated_at = excluded.updated_at
	`,
		group.ID,
		group.Name,
		group.Description,
		group.TotalCapacityBytes,
		group.DefaultMonthlyTrafficBytes,
		group.MaxFileSizeBytes,
		group.DailyUploadLimit,
		boolInt(group.AllowHotlink),
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return UserGroup{}, err
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, total_capacity_bytes, default_monthly_traffic_bytes,
			max_file_size_bytes, daily_upload_limit, allow_hotlink, created_at, updated_at
		FROM user_groups
		WHERE id = ?
	`, group.ID)
	return scanUserGroup(row)
}

func (s *SQLiteStore) UserUsage(ctx context.Context, userID string) (UserUsage, error) {
	user, err := s.UserByID(ctx, userID)
	if err != nil {
		return UserUsage{}, err
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, total_capacity_bytes, default_monthly_traffic_bytes,
			max_file_size_bytes, daily_upload_limit, allow_hotlink, created_at, updated_at
		FROM user_groups
		WHERE id = ?
	`, user.GroupID)
	group, err := scanUserGroup(row)
	if err != nil {
		return UserUsage{}, err
	}

	usage := UserUsage{User: user, Group: group}
	dayStart, dayEnd := utcDayBounds(time.Now())
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(size), 0)
		FROM resources
		WHERE owner_user_id = ? AND status = ?
	`, userID, string(resource.StatusActive)).Scan(&usage.UsedStorageBytes); err != nil {
		return UserUsage{}, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM resources
		WHERE owner_user_id = ? AND created_at >= ? AND created_at < ?
	`,
		userID,
		formatTime(dayStart),
		formatTime(dayEnd),
	).Scan(&usage.DailyUploadCount); err != nil {
		return UserUsage{}, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(traffic_bytes), 0)
		FROM resource_traffic_windows
		WHERE user_id = ? AND window_type = 'month' AND window_key = ?
	`, userID, time.Now().Format("2006-01")).Scan(&usage.MonthlyTrafficBytes); err != nil {
		return UserUsage{}, err
	}
	return usage, nil
}

func (s *SQLiteStore) AnonymousUsage(ctx context.Context, groupID string) (int64, int, error) {
	var usedStorageBytes int64
	var dailyUploadCount int
	dayStart, dayEnd := utcDayBounds(time.Now())
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(size), 0)
		FROM resources
		WHERE user_group = ? AND owner_user_id = '' AND status = ?
	`, groupID, string(resource.StatusActive)).Scan(&usedStorageBytes); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM resources
		WHERE user_group = ? AND owner_user_id = '' AND created_at >= ? AND created_at < ?
	`, groupID, formatTime(dayStart), formatTime(dayEnd)).Scan(&dailyUploadCount); err != nil {
		return 0, 0, err
	}
	return usedStorageBytes, dailyUploadCount, nil
}

func (s *SQLiteStore) ListUsers(ctx context.Context) ([]auth.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.role, u.user_group_id,
			COALESCE(g.name, u.user_group_id), u.status
		FROM users u
		LEFT JOIN user_groups g ON g.id = u.user_group_id
		ORDER BY u.created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []auth.User
	for rows.Next() {
		var user auth.User
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.GroupID, &user.GroupName, &user.Status); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *SQLiteStore) CreateUser(ctx context.Context, params CreateUserParams) (auth.User, error) {
	userID, err := newID("usr")
	if err != nil {
		return auth.User{}, err
	}
	now := time.Now()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO users (
			id, username, display_name, password_hash, role, user_group_id, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, params.Username, params.DisplayName, params.PasswordHash, params.Role, params.GroupID, params.Status, formatTime(now), formatTime(now)); err != nil {
		return auth.User{}, err
	}
	return s.UserByID(ctx, userID)
}

func (s *SQLiteStore) UpdateUser(ctx context.Context, params UpdateUserParams) (auth.User, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET display_name = ?, user_group_id = ?, status = ?, updated_at = ?
		WHERE id = ?
	`, params.DisplayName, params.GroupID, params.Status, formatTime(time.Now()), params.ID)
	if err != nil {
		return auth.User{}, err
	}
	if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected == 0 {
		return auth.User{}, auth.ErrUserNotFound
	}
	return s.UserByID(ctx, params.ID)
}

func (s *SQLiteStore) SetUserPassword(ctx context.Context, userID, passwordHash string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?
	`, passwordHash, formatTime(time.Now()), userID)
	if err != nil {
		return err
	}
	if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}

func (s *SQLiteStore) StorageConfigs(ctx context.Context) ([]StorageConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, name, endpoint, region, bucket, access_key_id, secret_access_key, username, password,
			public_base_url, base_path, use_path_style, is_default, created_at, updated_at
		FROM storage_configs
		ORDER BY is_default DESC, created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []StorageConfig
	for rows.Next() {
		cfg, err := scanStorageConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

func (s *SQLiteStore) UpsertStorageConfig(ctx context.Context, cfg StorageConfig) (StorageConfig, error) {
	if cfg.ID == "" {
		cfg.ID = strings.ToLower(strings.ReplaceAll(cfg.Name, " ", "-"))
		if cfg.ID == "" {
			cfg.ID = "storage"
		}
	}
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StorageConfig{}, err
	}
	defer tx.Rollback()

	if cfg.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE storage_configs SET is_default = 0, updated_at = ?`, formatTime(now)); err != nil {
			return StorageConfig{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO app_settings (key, value, updated_at)
			VALUES ('default_storage', ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, cfg.ID, formatTime(now)); err != nil {
			return StorageConfig{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO storage_configs (
			id, type, name, endpoint, region, bucket, access_key_id, secret_access_key, username, password,
			public_base_url, base_path, use_path_style, is_default, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			name = excluded.name,
			endpoint = excluded.endpoint,
			region = excluded.region,
			bucket = excluded.bucket,
			access_key_id = excluded.access_key_id,
			secret_access_key = excluded.secret_access_key,
			username = excluded.username,
			password = excluded.password,
			public_base_url = excluded.public_base_url,
			base_path = excluded.base_path,
			use_path_style = excluded.use_path_style,
			is_default = excluded.is_default,
			updated_at = excluded.updated_at
	`,
		cfg.ID, cfg.Type, cfg.Name, cfg.Endpoint, cfg.Region, cfg.Bucket, cfg.AccessKeyID, cfg.SecretAccessKey, cfg.Username, cfg.Password,
		cfg.PublicBaseURL, cfg.BasePath, boolInt(cfg.UsePathStyle), boolInt(cfg.IsDefault), formatTime(now), formatTime(now),
	); err != nil {
		return StorageConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return StorageConfig{}, err
	}
	return s.storageConfigByID(ctx, cfg.ID)
}

func (s *SQLiteStore) DefaultStorageConfig(ctx context.Context) (StorageConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT sc.id, sc.type, sc.name, sc.endpoint, sc.region, sc.bucket, sc.access_key_id, sc.secret_access_key, sc.username, sc.password,
			sc.public_base_url, sc.base_path, sc.use_path_style, sc.is_default, sc.created_at, sc.updated_at
		FROM storage_configs sc
		WHERE sc.is_default = 1
		ORDER BY sc.updated_at DESC
		LIMIT 1
	`)
	cfg, err := scanStorageConfig(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StorageConfig{ID: "local", Type: "local", Name: "本机存储", IsDefault: true}, nil
	}
	return cfg, err
}

func (s *SQLiteStore) SiteSettings(ctx context.Context) (SiteSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT site_name, external_base_url, allow_guest_uploads, show_stats_on_home, show_featured_on_home, updated_at
		FROM site_settings
		WHERE id = 1
	`)
	settings, err := scanSiteSettings(row)
	if errors.Is(err, sql.ErrNoRows) {
		if err := s.seedSiteSettings(ctx); err != nil {
			return SiteSettings{}, err
		}
		row = s.db.QueryRowContext(ctx, `
			SELECT site_name, external_base_url, allow_guest_uploads, show_stats_on_home, show_featured_on_home, updated_at
			FROM site_settings
			WHERE id = 1
		`)
		return scanSiteSettings(row)
	}
	return settings, err
}

func (s *SQLiteStore) UpdateSiteSettings(ctx context.Context, settings SiteSettings) (SiteSettings, error) {
	now := time.Now()
	settings.SiteName = strings.TrimSpace(settings.SiteName)
	settings.ExternalBaseURL = strings.TrimRight(strings.TrimSpace(settings.ExternalBaseURL), "/")
	if settings.SiteName == "" {
		settings.SiteName = "马赫环"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SiteSettings{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO site_settings (
			id, site_name, external_base_url, allow_guest_uploads, show_stats_on_home, show_featured_on_home, updated_at
		) VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			site_name = excluded.site_name,
			external_base_url = excluded.external_base_url,
			allow_guest_uploads = excluded.allow_guest_uploads,
			show_stats_on_home = excluded.show_stats_on_home,
			show_featured_on_home = excluded.show_featured_on_home,
			updated_at = excluded.updated_at
	`,
		settings.SiteName,
		settings.ExternalBaseURL,
		boolInt(settings.AllowGuestUploads),
		boolInt(settings.ShowStatsOnHome),
		boolInt(settings.ShowFeaturedOnHome),
		formatTime(now),
	); err != nil {
		return SiteSettings{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES ('site_name', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, settings.SiteName, formatTime(now)); err != nil {
		return SiteSettings{}, err
	}
	if err := tx.Commit(); err != nil {
		return SiteSettings{}, err
	}
	settings.UpdatedAt = now
	return settings, nil
}

func (s *SQLiteStore) FeaturedResources(ctx context.Context, includeInactive bool) ([]FeaturedResource, error) {
	clauses := []string{}
	args := []any{}
	if !includeInactive {
		clauses = append(clauses, `f.is_active = 1`, `r.status = ?`)
		args = append(args, string(resource.StatusActive))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.owner_user_id, r.owner_username, r.user_group, r.is_private, r.storage_driver, r.object_key, r.public_url, r.original_name,
			r.extension, r.resource_type, r.size, r.content_type, r.hash, r.status, r.access_count,
			r.traffic_bytes, r.created_at, r.updated_at, r.deleted_at, r.cache_control,
			r.disposition, r.monthly_limit, r.monthly_traffic, r.month_window, r.upload_ip, r.upload_user_agent,
			f.sort_order, f.created_at, f.updated_at
		FROM featured_resources f
		INNER JOIN resources r ON r.id = f.resource_id
	`+where+`
		ORDER BY f.sort_order ASC, f.created_at ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	featured := make([]FeaturedResource, 0)
	for rows.Next() {
		item, err := scanFeaturedResource(rows)
		if err != nil {
			return nil, err
		}
		featured = append(featured, item)
	}
	return featured, rows.Err()
}

func (s *SQLiteStore) AddFeaturedResource(ctx context.Context, resourceID string, sortOrder int) (FeaturedResource, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return FeaturedResource{}, ErrNotFound
	}
	record, err := s.Resource(ctx, resourceID)
	if err != nil {
		return FeaturedResource{}, err
	}
	if record.Status != resource.StatusActive {
		return FeaturedResource{}, ErrNotFound
	}
	if sortOrder <= 0 {
		if err := s.db.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(sort_order), 0) + 1
			FROM featured_resources
			WHERE is_active = 1
		`).Scan(&sortOrder); err != nil {
			return FeaturedResource{}, err
		}
	}
	now := time.Now()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO featured_resources (resource_id, sort_order, is_active, created_at, updated_at)
		VALUES (?, ?, 1, ?, ?)
		ON CONFLICT(resource_id) DO UPDATE SET
			sort_order = excluded.sort_order,
			is_active = 1,
			updated_at = excluded.updated_at
	`, resourceID, sortOrder, formatTime(now), formatTime(now)); err != nil {
		return FeaturedResource{}, err
	}
	return s.featuredResourceByID(ctx, resourceID)
}

func (s *SQLiteStore) RemoveFeaturedResource(ctx context.Context, resourceID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE featured_resources
		SET is_active = 0, updated_at = ?
		WHERE resource_id = ?
	`, formatTime(time.Now()), strings.TrimSpace(resourceID))
	if err != nil {
		return err
	}
	if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ReorderFeaturedResources(ctx context.Context, resourceIDs []string) ([]FeaturedResource, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now()
	for index, resourceID := range uniqueResourceIDs(resourceIDs) {
		result, err := tx.ExecContext(ctx, `
			UPDATE featured_resources
			SET sort_order = ?, is_active = 1, updated_at = ?
			WHERE resource_id = ?
		`, index+1, formatTime(now), resourceID)
		if err != nil {
			return nil, err
		}
		if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected == 0 {
			return nil, ErrNotFound
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.FeaturedResources(ctx, false)
}

func (s *SQLiteStore) SigningSecret(ctx context.Context) (string, error) {
	const key = "resource_signing_secret"

	var secret string
	err := s.db.QueryRowContext(ctx, `
		SELECT value
		FROM app_settings
		WHERE key = ?
	`, key).Scan(&secret)
	switch {
	case err == nil && strings.TrimSpace(secret) != "":
		return secret, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return "", err
	}

	secret, err = newSecretHex(32)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, secret, formatTime(time.Now())); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *SQLiteStore) storageConfigByID(ctx context.Context, id string) (StorageConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, name, endpoint, region, bucket, access_key_id, secret_access_key, username, password,
			public_base_url, base_path, use_path_style, is_default, created_at, updated_at
		FROM storage_configs
		WHERE id = ?
	`, id)
	return scanStorageConfig(row)
}

func (s *SQLiteStore) featuredResourceByID(ctx context.Context, resourceID string) (FeaturedResource, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT r.id, r.owner_user_id, r.owner_username, r.user_group, r.is_private, r.storage_driver, r.object_key, r.public_url, r.original_name,
			r.extension, r.resource_type, r.size, r.content_type, r.hash, r.status, r.access_count,
			r.traffic_bytes, r.created_at, r.updated_at, r.deleted_at, r.cache_control,
			r.disposition, r.monthly_limit, r.monthly_traffic, r.month_window, r.upload_ip, r.upload_user_agent,
			f.sort_order, f.created_at, f.updated_at
		FROM featured_resources f
		INNER JOIN resources r ON r.id = f.resource_id
		WHERE f.resource_id = ?
	`, resourceID)
	item, err := scanFeaturedResource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return FeaturedResource{}, ErrNotFound
	}
	return item, err
}

func (s *SQLiteStore) CreateResource(ctx context.Context, bundle CreateResourceBundle) error {
	record := bundle.Record
	if record.Status == "" {
		record.Status = resource.StatusActive
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO resources (
			id, owner_user_id, owner_username, user_group, is_private, storage_driver, object_key, public_url, original_name,
			extension, resource_type, size, content_type, hash, status, access_count,
			traffic_bytes, created_at, updated_at, deleted_at, cache_control,
			disposition, monthly_limit, monthly_traffic, month_window, upload_ip, upload_user_agent
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		record.ID,
		record.OwnerUserID,
		record.OwnerUsername,
		record.UserGroup,
		boolInt(record.IsPrivate),
		record.StorageDriver,
		record.ObjectKey,
		record.PublicURL,
		record.OriginalName,
		record.Extension,
		string(record.Type),
		record.Size,
		record.ContentType,
		record.Hash,
		string(record.Status),
		record.AccessCount,
		record.TrafficBytes,
		formatTime(record.CreatedAt),
		formatTime(record.UpdatedAt),
		formatTime(record.DeletedAt),
		record.CacheControl,
		record.Disposition,
		record.MonthlyLimit,
		record.MonthlyTraffic,
		record.MonthWindow,
		record.UploadIP,
		record.UploadUserAgent,
	); err != nil {
		return err
	}

	if bundle.Metadata.ResourceID != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO resource_metadata (
				resource_id, header_sha256, image_width, image_height, image_decoded, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(resource_id) DO UPDATE SET
				header_sha256 = excluded.header_sha256,
				image_width = excluded.image_width,
				image_height = excluded.image_height,
				image_decoded = excluded.image_decoded,
				updated_at = excluded.updated_at
		`,
			bundle.Metadata.ResourceID,
			bundle.Metadata.HeaderSHA256,
			bundle.Metadata.ImageWidth,
			bundle.Metadata.ImageHeight,
			boolInt(bundle.Metadata.ImageDecoded),
			formatTime(bundle.Metadata.CreatedAt),
			formatTime(bundle.Metadata.UpdatedAt),
		); err != nil {
			return err
		}
	}

	for _, variant := range bundle.Variants {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO resource_variants (
				id, resource_id, kind, storage_driver, object_key, content_type, size, width, height, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				kind = excluded.kind,
				storage_driver = excluded.storage_driver,
				object_key = excluded.object_key,
				content_type = excluded.content_type,
				size = excluded.size,
				width = excluded.width,
				height = excluded.height
		`,
			variant.ID,
			variant.ResourceID,
			variant.Kind,
			variant.StorageDriver,
			variant.ObjectKey,
			variant.ContentType,
			variant.Size,
			variant.Width,
			variant.Height,
			formatTime(variant.CreatedAt),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) ListResources(ctx context.Context, params resource.ListParams) (resource.ListResult, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}

	clauses := []string{}
	args := []any{}
	if !params.IncludeDeleted && params.Status == "" {
		clauses = append(clauses, `status != ?`)
		args = append(args, string(resource.StatusDeleted))
	}
	if params.Status != "" {
		clauses = append(clauses, `status = ?`)
		args = append(args, string(params.Status))
	}
	if params.Type != "" {
		clauses = append(clauses, `resource_type = ?`)
		args = append(args, string(params.Type))
	}
	if params.UserGroup != "" {
		clauses = append(clauses, `user_group = ?`)
		args = append(args, params.UserGroup)
	}
	if search := strings.TrimSpace(params.Search); search != "" {
		clauses = append(clauses, `(LOWER(original_name) LIKE ? OR LOWER(id) LIKE ? OR LOWER(extension) LIKE ?)`)
		like := "%" + strings.ToLower(search) + "%"
		args = append(args, like, like, like)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resources`+where, args...).Scan(&total); err != nil {
		return resource.ListResult{}, err
	}

	orderBy := ` ORDER BY created_at DESC`
	if params.Sort == "created_asc" {
		orderBy = ` ORDER BY created_at ASC`
	}

	query := `
		SELECT id, owner_user_id, owner_username, user_group, is_private, storage_driver, object_key, public_url, original_name,
			extension, resource_type, size, content_type, hash, status, access_count,
			traffic_bytes, created_at, updated_at, deleted_at, cache_control,
			disposition, monthly_limit, monthly_traffic, month_window, upload_ip, upload_user_agent
		FROM resources
	` + where + orderBy + ` LIMIT ? OFFSET ?`
	args = append(args, params.PageSize, (params.Page-1)*params.PageSize)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return resource.ListResult{}, err
	}
	defer rows.Close()

	records := make([]resource.Record, 0, params.PageSize)
	for rows.Next() {
		record, err := scanResource(rows)
		if err != nil {
			return resource.ListResult{}, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return resource.ListResult{}, err
	}

	totalPages := 0
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(params.PageSize)))
	}
	return resource.ListResult{
		Items:      records,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (s *SQLiteStore) Resource(ctx context.Context, id string) (resource.Record, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_user_id, owner_username, user_group, is_private, storage_driver, object_key, public_url, original_name,
			extension, resource_type, size, content_type, hash, status, access_count,
			traffic_bytes, created_at, updated_at, deleted_at, cache_control,
			disposition, monthly_limit, monthly_traffic, month_window, upload_ip, upload_user_agent
		FROM resources
		WHERE id = ?
	`, id)

	record, err := scanResource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return resource.Record{}, ErrNotFound
	}
	return record, err
}

func (s *SQLiteStore) ResourceDetail(ctx context.Context, id string) (resource.Detail, error) {
	record, err := s.Resource(ctx, id)
	if err != nil {
		return resource.Detail{}, err
	}

	detail := resource.Detail{
		Record: record,
		Links:  resource.BuildLinks(record.OriginalName, record.PublicURL, record.Type),
	}
	if metadata, err := s.resourceMetadata(ctx, id); err == nil {
		detail.Metadata = metadata
	} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrNotFound) {
		return resource.Detail{}, err
	}
	variants, err := s.resourceVariants(ctx, id)
	if err != nil {
		return resource.Detail{}, err
	}
	detail.Variants = variants
	windows, err := s.resourceTrafficWindows(ctx, id)
	if err != nil {
		return resource.Detail{}, err
	}
	detail.TrafficWindows = windows
	return detail, nil
}

func (s *SQLiteStore) UpdateResourceVisibility(ctx context.Context, id string, isPrivate bool) (resource.Record, error) {
	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return resource.Record{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE resources
		SET is_private = ?, updated_at = ?
		WHERE id = ?
	`, boolInt(isPrivate), formatTime(now), id)
	if err != nil {
		return resource.Record{}, err
	}
	if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected == 0 {
		return resource.Record{}, ErrNotFound
	}
	if isPrivate {
		if _, err := tx.ExecContext(ctx, `
			UPDATE featured_resources
			SET is_active = 0, updated_at = ?
			WHERE resource_id = ?
		`, formatTime(now), id); err != nil {
			return resource.Record{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return resource.Record{}, err
	}
	return s.Resource(ctx, id)
}

func (s *SQLiteStore) MarkResourceDeleted(ctx context.Context, id string) (resource.Record, error) {
	now := time.Now()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE resources SET status = ?, deleted_at = ?, updated_at = ? WHERE id = ?
	`, string(resource.StatusDeleted), formatTime(now), formatTime(now), id); err != nil {
		return resource.Record{}, err
	}
	return s.Resource(ctx, id)
}

func (s *SQLiteStore) RestoreResource(ctx context.Context, id string) (resource.Record, error) {
	now := time.Now()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE resources SET status = ?, deleted_at = '', updated_at = ? WHERE id = ?
	`, string(resource.StatusActive), formatTime(now), id); err != nil {
		return resource.Record{}, err
	}
	return s.Resource(ctx, id)
}

func (s *SQLiteStore) AddResourceTraffic(ctx context.Context, params AddTrafficParams) (resource.Record, error) {
	if params.AccessedAt.IsZero() {
		params.AccessedAt = time.Now()
	}
	record, err := s.Resource(ctx, params.ResourceID)
	if err != nil {
		return resource.Record{}, err
	}

	monthlyTraffic := record.MonthlyTraffic
	month := params.AccessedAt.Format("2006-01")
	if record.MonthWindow != month {
		monthlyTraffic = 0
	}
	monthlyTraffic += params.Bytes
	day := params.AccessedAt.Format("2006-01-02")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return resource.Record{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE resources
		SET access_count = access_count + 1,
			traffic_bytes = traffic_bytes + ?,
			monthly_traffic = ?,
			month_window = ?,
			updated_at = ?
		WHERE id = ?
	`, params.Bytes, monthlyTraffic, month, formatTime(params.AccessedAt), params.ResourceID); err != nil {
		return resource.Record{}, err
	}
	if err := s.bumpTrafficWindowTx(ctx, tx, params.ResourceID, params.UserID, record.Type, "day", day, params.Bytes, params.AccessedAt); err != nil {
		return resource.Record{}, err
	}
	if err := s.bumpTrafficWindowTx(ctx, tx, params.ResourceID, params.UserID, record.Type, "month", month, params.Bytes, params.AccessedAt); err != nil {
		return resource.Record{}, err
	}
	logID, err := newID("tlog")
	if err != nil {
		return resource.Record{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO traffic_logs (id, resource_id, user_id, resource_type, window_type, window_key, bytes, requested_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		logID,
		params.ResourceID,
		params.UserID,
		string(record.Type),
		"day",
		day,
		params.Bytes,
		formatTime(params.AccessedAt),
	); err != nil {
		return resource.Record{}, err
	}
	if err := tx.Commit(); err != nil {
		return resource.Record{}, err
	}
	return s.Resource(ctx, params.ResourceID)
}

func (s *SQLiteStore) ResourceStats(ctx context.Context) (resource.Stats, error) {
	var stats resource.Stats
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),
			COALESCE(SUM(CASE WHEN status = ? THEN size ELSE 0 END), 0),
			COALESCE(SUM(traffic_bytes), 0)
		FROM resources
	`, string(resource.StatusActive), string(resource.StatusActive)).Scan(
		&stats.TotalResources,
		&stats.ActiveResources,
		&stats.TotalStorageBytes,
		&stats.TotalTrafficBytes,
	); err != nil {
		return resource.Stats{}, err
	}

	dayStart, dayEnd := utcDayBounds(time.Now())
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM resources
		WHERE created_at >= ? AND created_at < ?
	`, formatTime(dayStart), formatTime(dayEnd)).Scan(&stats.TodayUploads); err != nil {
		return resource.Stats{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT window_key, COALESCE(SUM(traffic_bytes), 0)
		FROM resource_traffic_windows
		WHERE window_type = 'day' AND window_key >= ?
		GROUP BY window_key
		ORDER BY window_key ASC
	`, time.Now().AddDate(0, 0, -6).Format("2006-01-02"))
	if err != nil {
		return resource.Stats{}, err
	}
	defer rows.Close()

	byDay := map[string]int64{}
	for rows.Next() {
		var key string
		var bytes int64
		if err := rows.Scan(&key, &bytes); err != nil {
			return resource.Stats{}, err
		}
		byDay[key] = bytes
	}
	if err := rows.Err(); err != nil {
		return resource.Stats{}, err
	}

	points := make([]resource.TrafficPoint, 0, 7)
	for i := 6; i >= 0; i-- {
		key := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		points = append(points, resource.TrafficPoint{
			Label: key[5:],
			Bytes: byDay[key],
		})
	}
	stats.RecentTraffic = points
	return stats, nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	pragmas := []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA synchronous = NORMAL`,
		`PRAGMA temp_store = MEMORY`,
	}
	for _, pragma := range pragmas {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS user_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			total_capacity_bytes INTEGER NOT NULL DEFAULT 0,
			default_monthly_traffic_bytes INTEGER NOT NULL DEFAULT 0,
			max_file_size_bytes INTEGER NOT NULL DEFAULT 0,
			daily_upload_limit INTEGER NOT NULL DEFAULT 0,
			allow_hotlink INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL,
			user_group_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS site_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			site_name TEXT NOT NULL DEFAULT '马赫环',
			external_base_url TEXT NOT NULL DEFAULT '',
			allow_guest_uploads INTEGER NOT NULL DEFAULT 1,
			show_stats_on_home INTEGER NOT NULL DEFAULT 1,
			show_featured_on_home INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS storage_configs (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			endpoint TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			bucket TEXT NOT NULL DEFAULT '',
			access_key_id TEXT NOT NULL DEFAULT '',
			secret_access_key TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			password TEXT NOT NULL DEFAULT '',
			public_base_url TEXT NOT NULL DEFAULT '',
			base_path TEXT NOT NULL DEFAULT '',
			use_path_style INTEGER NOT NULL DEFAULT 1,
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS policy_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			is_active INTEGER NOT NULL DEFAULT 0,
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS policy_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			policy_group_id TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL DEFAULT 0,
			user_group TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			extension TEXT NOT NULL DEFAULT '',
			allow_upload INTEGER NOT NULL DEFAULT 0,
			allow_access INTEGER NOT NULL DEFAULT 0,
			max_file_size_bytes INTEGER NOT NULL DEFAULT 0,
			monthly_traffic_per_resource_bytes INTEGER NOT NULL DEFAULT 0,
			monthly_traffic_per_user_and_type_bytes INTEGER NOT NULL DEFAULT 0,
			require_auth INTEGER NOT NULL DEFAULT 0,
			require_review INTEGER NOT NULL DEFAULT 0,
			force_private INTEGER NOT NULL DEFAULT 0,
			cache_control TEXT NOT NULL DEFAULT '',
			download_disposition TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS resources (
			id TEXT PRIMARY KEY,
			owner_user_id TEXT NOT NULL DEFAULT '',
			owner_username TEXT NOT NULL DEFAULT '',
			user_group TEXT NOT NULL,
			is_private INTEGER NOT NULL DEFAULT 0,
			storage_driver TEXT NOT NULL,
			object_key TEXT NOT NULL,
			public_url TEXT NOT NULL,
			original_name TEXT NOT NULL,
			extension TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			size INTEGER NOT NULL,
			content_type TEXT NOT NULL,
			hash TEXT NOT NULL,
			status TEXT NOT NULL,
			access_count INTEGER NOT NULL DEFAULT 0,
			traffic_bytes INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			deleted_at TEXT NOT NULL DEFAULT '',
			cache_control TEXT NOT NULL DEFAULT '',
			disposition TEXT NOT NULL DEFAULT '',
			monthly_limit INTEGER NOT NULL DEFAULT 0,
			monthly_traffic INTEGER NOT NULL DEFAULT 0,
			month_window TEXT NOT NULL DEFAULT '',
			upload_ip TEXT NOT NULL DEFAULT '',
			upload_user_agent TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS resource_metadata (
			resource_id TEXT PRIMARY KEY,
			header_sha256 TEXT NOT NULL DEFAULT '',
			image_width INTEGER NOT NULL DEFAULT 0,
			image_height INTEGER NOT NULL DEFAULT 0,
			image_decoded INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS resource_variants (
			id TEXT PRIMARY KEY,
			resource_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			storage_driver TEXT NOT NULL,
			object_key TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size INTEGER NOT NULL DEFAULT 0,
			width INTEGER NOT NULL DEFAULT 0,
			height INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS resource_traffic_windows (
			resource_id TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			resource_type TEXT NOT NULL,
			window_type TEXT NOT NULL,
			window_key TEXT NOT NULL,
			request_count INTEGER NOT NULL DEFAULT 0,
			traffic_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (resource_id, user_id, window_type, window_key)
		);

		CREATE TABLE IF NOT EXISTS traffic_logs (
			id TEXT PRIMARY KEY,
			resource_id TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			resource_type TEXT NOT NULL,
			window_type TEXT NOT NULL,
			window_key TEXT NOT NULL,
			bytes INTEGER NOT NULL DEFAULT 0,
			requested_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS featured_resources (
			resource_id TEXT PRIMARY KEY,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE INDEX IF NOT EXISTS resources_status_created_idx ON resources(status, created_at);
		CREATE INDEX IF NOT EXISTS resources_type_created_idx ON resources(resource_type, created_at);
		CREATE INDEX IF NOT EXISTS resources_group_created_idx ON resources(user_group, created_at);
		CREATE INDEX IF NOT EXISTS resources_owner_created_idx ON resources(owner_user_id, created_at);
		CREATE INDEX IF NOT EXISTS policy_rules_group_position_idx ON policy_rules(policy_group_id, position, id);
		CREATE INDEX IF NOT EXISTS resource_variants_resource_idx ON resource_variants(resource_id, created_at);
		CREATE INDEX IF NOT EXISTS resource_traffic_windows_window_idx ON resource_traffic_windows(window_type, window_key);
		CREATE INDEX IF NOT EXISTS traffic_logs_resource_requested_idx ON traffic_logs(resource_id, requested_at);
		CREATE INDEX IF NOT EXISTS featured_resources_active_sort_idx ON featured_resources(is_active, sort_order, updated_at);
	`)
	if err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "policy_rules", "policy_group_id", `ALTER TABLE policy_rules ADD COLUMN policy_group_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "resources", "upload_ip", `ALTER TABLE resources ADD COLUMN upload_ip TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "resources", "upload_user_agent", `ALTER TABLE resources ADD COLUMN upload_user_agent TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "resources", "owner_user_id", `ALTER TABLE resources ADD COLUMN owner_user_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "resources", "is_private", `ALTER TABLE resources ADD COLUMN is_private INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "resources", "owner_username", `ALTER TABLE resources ADD COLUMN owner_username TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "user_groups", "allow_hotlink", `ALTER TABLE user_groups ADD COLUMN allow_hotlink INTEGER NOT NULL DEFAULT 1`); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "storage_configs", "username", `ALTER TABLE storage_configs ADD COLUMN username TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, s.db, "storage_configs", "password", `ALTER TABLE storage_configs ADD COLUMN password TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) seedUserGroups(ctx context.Context) error {
	now := time.Now()
	return s.seedUserGroupsTx(ctx, s.db, now)
}

func (s *SQLiteStore) seedUserGroupsTx(ctx context.Context, exec execContext, now time.Time) error {
	groups := []struct {
		ID          string
		Name        string
		Description string
	}{
		{ID: policy.GroupGuest, Name: "游客", Description: "未登录访问者"},
		{ID: policy.GroupUser, Name: "登录用户", Description: "普通登录用户"},
		{ID: policy.GroupAdmin, Name: "管理员", Description: "系统管理员"},
	}

	for _, group := range groups {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO user_groups (id, name, description, allow_hotlink, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				description = excluded.description,
				updated_at = excluded.updated_at
		`, group.ID, group.Name, group.Description, formatTime(now), formatTime(now)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) seedPolicyGroups(ctx context.Context) error {
	now := time.Now()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO policy_groups (id, name, description, is_active, is_default, created_at, updated_at)
		VALUES (?, ?, ?, 1, 1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			is_default = 1
	`, policy.DefaultGroupID, policy.DefaultGroupName, "系统默认策略组", formatTime(now), formatTime(now)); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE policy_rules
		SET policy_group_id = ?
		WHERE policy_group_id = ''
	`, policy.DefaultGroupID); err != nil {
		return err
	}

	var activeCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM policy_groups WHERE is_active = 1`).Scan(&activeCount); err != nil {
		return err
	}
	if activeCount == 0 {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE policy_groups
			SET is_active = CASE WHEN id = ? THEN 1 ELSE 0 END,
				updated_at = ?
		`, policy.DefaultGroupID, formatTime(now)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) seedRules(ctx context.Context, defaultRules []policy.Rule) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM policy_rules WHERE policy_group_id = ?`, policy.DefaultGroupID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return s.replaceRulesForGroup(ctx, policy.DefaultGroupID, defaultRules)
}

func (s *SQLiteStore) seedSiteSettings(ctx context.Context) error {
	now := time.Now()
	state, err := s.InstallState(ctx)
	if err != nil {
		return err
	}
	siteName := strings.TrimSpace(state.SiteName)
	if siteName == "" {
		siteName = "马赫环"
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO site_settings (
			id, site_name, external_base_url, allow_guest_uploads, show_stats_on_home, show_featured_on_home, updated_at
		) VALUES (1, ?, '', 1, 1, 1, ?)
		ON CONFLICT(id) DO NOTHING
	`, siteName, formatTime(now))
	return err
}

func (s *SQLiteStore) importLegacyJSON(ctx context.Context, sqlitePath string) error {
	var resourceCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resources`).Scan(&resourceCount); err != nil {
		return err
	}
	if resourceCount > 0 {
		return nil
	}

	jsonPath := strings.TrimSuffix(sqlitePath, filepath.Ext(sqlitePath)) + ".json"
	file, err := os.Open(jsonPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	var legacy dataFile
	if err := json.NewDecoder(file).Decode(&legacy); err != nil {
		return err
	}
	if len(legacy.Rules) > 0 {
		if err := s.replaceRulesForGroup(ctx, policy.DefaultGroupID, legacy.Rules); err != nil {
			return err
		}
	}
	for _, record := range legacy.Resources {
		if err := s.CreateResource(ctx, CreateResourceBundle{Record: record}); err != nil {
			return err
		}
	}
	return nil
}

type boolInt bool

func (b boolInt) Value() (driver.Value, error) {
	if b {
		return int64(1), nil
	}
	return int64(0), nil
}

func (b *boolInt) Scan(value any) error {
	switch typed := value.(type) {
	case int64:
		*b = typed != 0
	case bool:
		*b = boolInt(typed)
	default:
		*b = false
	}
	return nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

type scanner interface {
	Scan(dest ...any) error
}

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func ensureSQLiteColumn(ctx context.Context, db *sql.DB, table, column, alterSQL string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, column) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, alterSQL)
	return err
}

func (s *SQLiteStore) resourceMetadata(ctx context.Context, resourceID string) (resource.StoredMetadata, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT resource_id, header_sha256, image_width, image_height, image_decoded, created_at, updated_at
		FROM resource_metadata
		WHERE resource_id = ?
	`, resourceID)
	metadata, err := scanResourceMetadata(row)
	if errors.Is(err, sql.ErrNoRows) {
		return resource.StoredMetadata{}, ErrNotFound
	}
	return metadata, err
}

func (s *SQLiteStore) resourceVariants(ctx context.Context, resourceID string) ([]resource.Variant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, resource_id, kind, storage_driver, object_key, content_type, size, width, height, created_at
		FROM resource_variants
		WHERE resource_id = ?
		ORDER BY created_at ASC, id ASC
	`, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var variants []resource.Variant
	for rows.Next() {
		variant, err := scanResourceVariant(rows)
		if err != nil {
			return nil, err
		}
		variants = append(variants, variant)
	}
	return variants, rows.Err()
}

func (s *SQLiteStore) resourceTrafficWindows(ctx context.Context, resourceID string) ([]resource.TrafficWindow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT resource_id, user_id, resource_type, window_type, window_key, request_count, traffic_bytes, updated_at
		FROM resource_traffic_windows
		WHERE resource_id = ?
		ORDER BY window_type ASC, window_key DESC
	`, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var windows []resource.TrafficWindow
	for rows.Next() {
		window, err := scanTrafficWindow(rows)
		if err != nil {
			return nil, err
		}
		windows = append(windows, window)
	}
	return windows, rows.Err()
}

func (s *SQLiteStore) bumpTrafficWindowTx(ctx context.Context, tx *sql.Tx, resourceID, userID string, resourceType resource.Type, windowType, windowKey string, bytes int64, updatedAt time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO resource_traffic_windows (
			resource_id, user_id, resource_type, window_type, window_key, request_count, traffic_bytes, updated_at
		) VALUES (?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(resource_id, user_id, window_type, window_key) DO UPDATE SET
			resource_type = excluded.resource_type,
			request_count = resource_traffic_windows.request_count + 1,
			traffic_bytes = resource_traffic_windows.traffic_bytes + excluded.traffic_bytes,
			updated_at = excluded.updated_at
	`, resourceID, userID, string(resourceType), windowType, windowKey, bytes, formatTime(updatedAt)); err != nil {
		return err
	}
	return nil
}

func scanSiteSettings(row scanner) (SiteSettings, error) {
	var settings SiteSettings
	var allowGuestUploads boolInt
	var showStatsOnHome boolInt
	var showFeaturedOnHome boolInt
	var updatedAt string
	if err := row.Scan(
		&settings.SiteName,
		&settings.ExternalBaseURL,
		&allowGuestUploads,
		&showStatsOnHome,
		&showFeaturedOnHome,
		&updatedAt,
	); err != nil {
		return SiteSettings{}, err
	}
	settings.AllowGuestUploads = bool(allowGuestUploads)
	settings.ShowStatsOnHome = bool(showStatsOnHome)
	settings.ShowFeaturedOnHome = bool(showFeaturedOnHome)
	settings.UpdatedAt = parseTime(updatedAt)
	return settings, nil
}

func scanResource(row scanner) (resource.Record, error) {
	var record resource.Record
	var isPrivate boolInt
	var resourceType string
	var status string
	var createdAt string
	var updatedAt string
	var deletedAt string
	if err := row.Scan(
		&record.ID,
		&record.OwnerUserID,
		&record.OwnerUsername,
		&record.UserGroup,
		&isPrivate,
		&record.StorageDriver,
		&record.ObjectKey,
		&record.PublicURL,
		&record.OriginalName,
		&record.Extension,
		&resourceType,
		&record.Size,
		&record.ContentType,
		&record.Hash,
		&status,
		&record.AccessCount,
		&record.TrafficBytes,
		&createdAt,
		&updatedAt,
		&deletedAt,
		&record.CacheControl,
		&record.Disposition,
		&record.MonthlyLimit,
		&record.MonthlyTraffic,
		&record.MonthWindow,
		&record.UploadIP,
		&record.UploadUserAgent,
	); err != nil {
		return resource.Record{}, err
	}

	record.Type = resource.Type(resourceType)
	record.IsPrivate = bool(isPrivate)
	record.Status = resource.Status(status)
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	record.DeletedAt = parseTime(deletedAt)
	return record, nil
}

func scanFeaturedResource(row scanner) (FeaturedResource, error) {
	item := FeaturedResource{}
	var isPrivate boolInt
	var resourceType string
	var status string
	var resourceCreatedAt string
	var resourceUpdatedAt string
	var resourceDeletedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&item.Resource.ID,
		&item.Resource.OwnerUserID,
		&item.Resource.OwnerUsername,
		&item.Resource.UserGroup,
		&isPrivate,
		&item.Resource.StorageDriver,
		&item.Resource.ObjectKey,
		&item.Resource.PublicURL,
		&item.Resource.OriginalName,
		&item.Resource.Extension,
		&resourceType,
		&item.Resource.Size,
		&item.Resource.ContentType,
		&item.Resource.Hash,
		&status,
		&item.Resource.AccessCount,
		&item.Resource.TrafficBytes,
		&resourceCreatedAt,
		&resourceUpdatedAt,
		&resourceDeletedAt,
		&item.Resource.CacheControl,
		&item.Resource.Disposition,
		&item.Resource.MonthlyLimit,
		&item.Resource.MonthlyTraffic,
		&item.Resource.MonthWindow,
		&item.Resource.UploadIP,
		&item.Resource.UploadUserAgent,
		&item.SortOrder,
		&createdAt,
		&updatedAt,
	); err != nil {
		return FeaturedResource{}, err
	}
	item.Resource.IsPrivate = bool(isPrivate)
	item.Resource.Type = resource.Type(resourceType)
	item.Resource.Status = resource.Status(status)
	item.Resource.CreatedAt = parseTime(resourceCreatedAt)
	item.Resource.UpdatedAt = parseTime(resourceUpdatedAt)
	item.Resource.DeletedAt = parseTime(resourceDeletedAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanResourceMetadata(row scanner) (resource.StoredMetadata, error) {
	var metadata resource.StoredMetadata
	var imageDecoded boolInt
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&metadata.ResourceID,
		&metadata.HeaderSHA256,
		&metadata.ImageWidth,
		&metadata.ImageHeight,
		&imageDecoded,
		&createdAt,
		&updatedAt,
	); err != nil {
		return resource.StoredMetadata{}, err
	}
	metadata.ImageDecoded = bool(imageDecoded)
	metadata.CreatedAt = parseTime(createdAt)
	metadata.UpdatedAt = parseTime(updatedAt)
	return metadata, nil
}

func scanUserGroup(row scanner) (UserGroup, error) {
	var group UserGroup
	var allowHotlink boolInt
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&group.ID,
		&group.Name,
		&group.Description,
		&group.TotalCapacityBytes,
		&group.DefaultMonthlyTrafficBytes,
		&group.MaxFileSizeBytes,
		&group.DailyUploadLimit,
		&allowHotlink,
		&createdAt,
		&updatedAt,
	); err != nil {
		return UserGroup{}, err
	}
	group.AllowHotlink = bool(allowHotlink)
	group.CreatedAt = parseTime(createdAt)
	group.UpdatedAt = parseTime(updatedAt)
	return group, nil
}

func scanResourceVariant(row scanner) (resource.Variant, error) {
	var variant resource.Variant
	var createdAt string
	if err := row.Scan(
		&variant.ID,
		&variant.ResourceID,
		&variant.Kind,
		&variant.StorageDriver,
		&variant.ObjectKey,
		&variant.ContentType,
		&variant.Size,
		&variant.Width,
		&variant.Height,
		&createdAt,
	); err != nil {
		return resource.Variant{}, err
	}
	variant.CreatedAt = parseTime(createdAt)
	return variant, nil
}

func scanStorageConfig(row scanner) (StorageConfig, error) {
	var cfg StorageConfig
	var usePathStyle boolInt
	var isDefault boolInt
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&cfg.ID,
		&cfg.Type,
		&cfg.Name,
		&cfg.Endpoint,
		&cfg.Region,
		&cfg.Bucket,
		&cfg.AccessKeyID,
		&cfg.SecretAccessKey,
		&cfg.Username,
		&cfg.Password,
		&cfg.PublicBaseURL,
		&cfg.BasePath,
		&usePathStyle,
		&isDefault,
		&createdAt,
		&updatedAt,
	); err != nil {
		return StorageConfig{}, err
	}
	cfg.UsePathStyle = bool(usePathStyle)
	cfg.IsDefault = bool(isDefault)
	cfg.CreatedAt = parseTime(createdAt)
	cfg.UpdatedAt = parseTime(updatedAt)
	return cfg, nil
}

func scanTrafficWindow(row scanner) (resource.TrafficWindow, error) {
	var window resource.TrafficWindow
	var resourceType string
	var updatedAt string
	if err := row.Scan(
		&window.ResourceID,
		&window.UserID,
		&resourceType,
		&window.WindowType,
		&window.WindowKey,
		&window.RequestCount,
		&window.TrafficBytes,
		&updatedAt,
	); err != nil {
		return resource.TrafficWindow{}, err
	}
	window.ResourceType = resource.Type(resourceType)
	window.UpdatedAt = parseTime(updatedAt)
	return window, nil
}

func scanPolicyGroup(row scanner) (policy.Group, error) {
	var group policy.Group
	var isActive boolInt
	var isDefault boolInt
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&group.ID,
		&group.Name,
		&group.Description,
		&isActive,
		&isDefault,
		&createdAt,
		&updatedAt,
	); err != nil {
		return policy.Group{}, err
	}
	group.IsActive = bool(isActive)
	group.IsDefault = bool(isDefault)
	group.CreatedAt = parseTime(createdAt)
	group.UpdatedAt = parseTime(updatedAt)
	return group, nil
}

func newID(prefix string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buf[:])), nil
}

func newSecretHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func uniqueResourceIDs(resourceIDs []string) []string {
	unique := make([]string, 0, len(resourceIDs))
	seen := make(map[string]struct{}, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		resourceID = strings.TrimSpace(resourceID)
		if resourceID == "" {
			continue
		}
		if _, ok := seen[resourceID]; ok {
			continue
		}
		seen[resourceID] = struct{}{}
		unique = append(unique, resourceID)
	}
	return unique
}

func utcDayBounds(now time.Time) (time.Time, time.Time) {
	start := now.UTC().Truncate(24 * time.Hour)
	return start, start.Add(24 * time.Hour)
}
