package store

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (s *PostgresStore) AuthenticateUser(input UserAuthInput) (User, bool, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" || input.Password == "" {
		return User{}, false, nil
	}
	row := s.db.QueryRow(`
		select u.id,u.tenant_id,t.name,u.email,coalesce(u.display_name,''),u.password_hash,u.role,u.status,u.auth_provider,coalesce(u.time_zone,''),u.theme,u.is_system_admin,u.must_change_password
		from users u join resource_scopes t on t.id=u.tenant_id
		where lower(u.email)=$1 and u.status='active' and (u.is_system_admin or t.status='active')
	`, email)
	var user User
	var passwordHash string
	if err := row.Scan(&user.ID, &user.TenantID, &user.TenantName, &user.Email, &user.DisplayName, &passwordHash, &user.Role, &user.Status, &user.AuthProvider, &user.TimeZone, &user.Theme, &user.SystemAdmin, &user.MustChangePassword); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, false, nil
		}
		return User{}, false, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.Password)); err != nil {
		return User{}, false, nil
	}
	return user, true, nil
}

func (s *PostgresStore) CreateUser(tenantID, email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u := User{ID: newID(), TenantID: tenantID, Email: email, Role: "operator", Status: "active", AuthProvider: "password", Theme: "light", MustChangePassword: true}
	_, err = s.db.Exec(`insert into users (id, tenant_id, email, password_hash, role, status, auth_provider, must_change_password) values ($1,$2,$3,$4,$5,$6,$7,true)`, u.ID, u.TenantID, u.Email, string(hash), u.Role, u.Status, u.AuthProvider)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, ErrUserExists
		}
		return User{}, err
	}
	return u, nil
}

func (s *PostgresStore) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`select u.id,u.tenant_id,t.name,u.email,coalesce(u.display_name,''),u.role,u.status,u.auth_provider,coalesce(u.time_zone,''),u.theme,u.is_system_admin,u.must_change_password from users u join resource_scopes t on t.id=u.tenant_id order by case when u.is_system_admin then 0 else 1 end, u.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.TenantName, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.AuthProvider, &u.TimeZone, &u.Theme, &u.SystemAdmin, &u.MustChangePassword); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *PostgresStore) GetUser(id string) (User, bool, error) {
	var u User
	err := s.db.QueryRow(`select u.id,u.tenant_id,t.name,u.email,coalesce(u.display_name,''),u.role,u.status,u.auth_provider,coalesce(u.time_zone,''),u.theme,u.is_system_admin,u.must_change_password from users u join resource_scopes t on t.id=u.tenant_id where u.id=$1`, id).Scan(&u.ID, &u.TenantID, &u.TenantName, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.AuthProvider, &u.TimeZone, &u.Theme, &u.SystemAdmin, &u.MustChangePassword)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	return u, err == nil, err
}

func (s *PostgresStore) UpdateUser(input UserUpdateInput) (User, bool, error) {
	result, err := s.db.Exec(`update users set tenant_id=case when is_system_admin then tenant_id else $2 end,email=case when is_system_admin then email else $3 end,display_name=nullif($4,''),role=case when is_system_admin then role else $5 end,status=case when is_system_admin then status else $6 end,time_zone=nullif($7,''),updated_at=now() where id=$1`, input.ID, input.TenantID, strings.ToLower(strings.TrimSpace(input.Email)), strings.TrimSpace(input.DisplayName), input.Role, input.Status, strings.TrimSpace(input.TimeZone))
	if err != nil {
		return User{}, false, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return User{}, false, nil
	}
	return s.GetUser(input.ID)
}

func (s *PostgresStore) SetUserTheme(id, theme string) (User, bool, error) {
	if theme != "light" && theme != "dark" {
		return User{}, false, ErrInvalidTheme
	}
	result, err := s.db.Exec(`update users set theme=$2,updated_at=now() where id=$1`, id, theme)
	if err != nil {
		return User{}, false, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return User{}, false, nil
	}
	return s.GetUser(id)
}

func (s *PostgresStore) DeleteUser(id string) (bool, error) {
	result, err := s.db.Exec(`delete from users where id=$1 and not is_system_admin`, id)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

func (s *PostgresStore) SetUserPassword(id, password string, mustChangePassword bool) (User, bool, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, false, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, false, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`update users set password_hash=$2,must_change_password=$3,updated_at=now() where id=$1`, id, string(hash), mustChangePassword)
	if err != nil {
		return User{}, false, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return User{}, false, nil
	}
	if _, err = tx.Exec(`delete from platform_sessions where user_id=$1`, id); err != nil {
		return User{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, false, err
	}
	return s.GetUser(id)
}

func (s *PostgresStore) GetAdminRecoveryEmail(userID string) (string, bool, error) {
	var email string
	err := s.db.QueryRow(`select email from admin_recovery_email where user_id=$1`, userID).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return email, err == nil, err
}
func (s *PostgresStore) SetAdminRecoveryEmail(userID, email string) (string, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	result, err := s.db.Exec(`insert into admin_recovery_email(user_id,email) select id,$2 from users where id=$1 and is_system_admin on conflict(user_id) do update set email=excluded.email,verified_at=now(),updated_at=now()`, userID, email)
	if err != nil {
		return "", false, err
	}
	n, _ := result.RowsAffected()
	return email, n == 1, nil
}

func (s *PostgresStore) CreatePlatformSession(userID string, ttl time.Duration) (PlatformSession, error) {
	token := "hcs_" + newID() + newID()
	expires := time.Now().UTC().Add(ttl)
	_, err := s.db.Exec(`insert into platform_sessions(id,user_id,token_hash,expires_at) values($1,$2,$3,$4)`, newID(), userID, resetTokenDigest(token), expires)
	return PlatformSession{Token: token, UserID: userID, ExpiresAt: expires}, err
}

func (s *PostgresStore) AuthenticatePlatformSession(token string) (User, bool, error) {
	var u User
	err := s.db.QueryRow(`select u.id,u.tenant_id,t.name,u.email,coalesce(u.display_name,''),u.role,u.status,u.auth_provider,coalesce(u.time_zone,''),u.theme,u.is_system_admin,u.must_change_password from platform_sessions s join users u on u.id=s.user_id join resource_scopes t on t.id=u.tenant_id where s.token_hash=$1 and s.expires_at>now() and u.status='active' and (u.is_system_admin or t.status='active')`, resetTokenDigest(token)).Scan(&u.ID, &u.TenantID, &u.TenantName, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.AuthProvider, &u.TimeZone, &u.Theme, &u.SystemAdmin, &u.MustChangePassword)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	_, _ = s.db.Exec(`update platform_sessions set last_seen_at=now() where token_hash=$1`, resetTokenDigest(token))
	return u, true, nil
}

func (s *PostgresStore) DeletePlatformSession(token string) error {
	_, err := s.db.Exec(`delete from platform_sessions where token_hash=$1`, resetTokenDigest(token))
	return err
}

func (s *PostgresStore) CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	token := "hpr_" + newID() + newID()
	result, err := s.db.Exec(`insert into password_reset_tokens (id,user_id,token_hash,expires_at) select $1,u.id,$2,$3 from users u join resource_scopes t on t.id=u.tenant_id left join admin_recovery_email recovery on recovery.user_id=u.id where (lower(u.email)=$4 and not u.is_system_admin or lower(recovery.email)=$4 and u.is_system_admin) and u.status='active' and (u.is_system_admin or t.status='active')`, newID(), resetTokenDigest(token), time.Now().UTC().Add(ttl), email)
	if err != nil {
		return "", false, err
	}
	n, _ := result.RowsAffected()
	return token, n > 0, nil
}

func (s *PostgresStore) ResetPassword(token, password string) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var u User
	digest := resetTokenDigest(token)
	err = tx.QueryRow(`update users u set password_hash=$1,must_change_password=false,updated_at=now() from password_reset_tokens t where t.user_id=u.id and t.token_hash=$2 and t.used_at is null and t.expires_at>now() returning u.id,u.tenant_id,u.email,u.role,u.status`, string(hash), digest).Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &u.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrResetInvalid
	}
	if err != nil {
		return User{}, err
	}
	if _, err = tx.Exec(`update password_reset_tokens set used_at=now() where token_hash=$1`, digest); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func resetTokenDigest(token string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(token))) }

func (s *PostgresStore) FindOrCreateGoogleUser(email string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	err := s.db.QueryRow(`select id,tenant_id,email,role,status,coalesce(time_zone,''),theme from users where tenant_id=$1 and email=$2`, DefaultTenantID, email).Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &u.Status, &u.TimeZone, &u.Theme)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	randomPassword := "google:" + newID() + newID()
	u, err = s.CreateUser(DefaultTenantID, email, randomPassword)
	if err != nil {
		return User{}, err
	}
	_, err = s.db.Exec(`update users set auth_provider='google',role='operator' where id=$1`, u.ID)
	if err != nil {
		return User{}, err
	}
	return s.GetUserValue(u.ID)
}

func (s *PostgresStore) GetUserValue(id string) (User, error) {
	u, ok, err := s.GetUser(id)
	if err != nil {
		return User{}, err
	}
	if !ok {
		return User{}, sql.ErrNoRows
	}
	return u, nil
}
