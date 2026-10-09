package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func (s *PostgresStore) GetEmailSettings() (EmailSettings, bool, error) {
	return scanEmailSettings(s.db.QueryRow(emailSettingsSelect + ` where is_default=true`))
}

const emailSettingsSelect = `select id::text,name,is_default,enabled,host,port,security,username,password_ciphertext,sender_name,sender_email,last_test_status,last_tested_at,last_test_error,created_at,updated_at from smtp_configurations`

type emailSettingsScanner interface{ Scan(...any) error }

func scanEmailSettings(scanner emailSettingsScanner) (EmailSettings, bool, error) {
	var item EmailSettings
	err := scanner.Scan(&item.ID, &item.Name, &item.IsDefault, &item.Enabled, &item.Host, &item.Port, &item.Security, &item.Username, &item.PasswordCiphertext, &item.SenderName, &item.SenderEmail, &item.LastTestStatus, &item.LastTestedAt, &item.LastTestError, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return EmailSettings{}, false, nil
	}
	item.PasswordConfigured = item.PasswordCiphertext != ""
	return item, err == nil, err
}
func (s *PostgresStore) UpsertEmailSettings(input EmailSettingsInput) (EmailSettings, error) {
	current, found, err := s.GetEmailSettings()
	if err != nil {
		return EmailSettings{}, err
	}
	if found {
		item, _, updateErr := s.UpdateEmailSettings(current.ID, EmailSettingsInput{Name: current.Name, Enabled: input.Enabled, Host: input.Host, Port: input.Port, Security: input.Security, Username: input.Username, PasswordCiphertext: input.PasswordCiphertext, SenderName: input.SenderName, SenderEmail: input.SenderEmail, UpdatedBy: input.UpdatedBy})
		return item, updateErr
	}
	return s.CreateEmailSettings(EmailSettingsInput{Name: defaultSMTPName(input.Name), Enabled: input.Enabled, Host: input.Host, Port: input.Port, Security: input.Security, Username: input.Username, PasswordCiphertext: input.PasswordCiphertext, SenderName: input.SenderName, SenderEmail: input.SenderEmail, UpdatedBy: input.UpdatedBy})
}

func (s *PostgresStore) ListEmailSettings() ([]EmailSettings, error) {
	rows, err := s.db.Query(emailSettingsSelect + ` order by is_default desc,lower(name),created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []EmailSettings{}
	for rows.Next() {
		item, _, scanErr := scanEmailSettings(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) GetEmailSettingsByID(id string) (EmailSettings, bool, error) {
	return scanEmailSettings(s.db.QueryRow(emailSettingsSelect+` where id=$1`, id))
}

func emailSettingsNameConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "smtp_configurations_name_unique" {
		return ErrEmailSettingsNameExists
	}
	return err
}

func (s *PostgresStore) CreateEmailSettings(input EmailSettingsInput) (EmailSettings, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return EmailSettings{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`lock table smtp_configurations in share row exclusive mode`); err != nil {
		return EmailSettings{}, err
	}
	var count int
	if err = tx.QueryRow(`select count(*) from smtp_configurations`).Scan(&count); err != nil {
		return EmailSettings{}, err
	}
	id := NewPublicID()
	_, err = tx.Exec(`insert into smtp_configurations(id,name,is_default,enabled,host,port,security,username,password_ciphertext,sender_name,sender_email,updated_by) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,nullif($12,'')::uuid)`, id, strings.TrimSpace(input.Name), count == 0, input.Enabled, input.Host, input.Port, input.Security, input.Username, input.PasswordCiphertext, input.SenderName, input.SenderEmail, input.UpdatedBy)
	if err != nil {
		return EmailSettings{}, emailSettingsNameConflict(err)
	}
	if err = tx.Commit(); err != nil {
		return EmailSettings{}, err
	}
	item, _, err := s.GetEmailSettingsByID(id)
	return item, err
}

func (s *PostgresStore) UpdateEmailSettings(id string, input EmailSettingsInput) (EmailSettings, bool, error) {
	result, err := s.db.Exec(`update smtp_configurations set name=$2,enabled=$3,host=$4,port=$5,security=$6,username=$7,password_ciphertext=$8,sender_name=$9,sender_email=$10,updated_by=nullif($11,'')::uuid,updated_at=now() where id=$1`, id, strings.TrimSpace(input.Name), input.Enabled, input.Host, input.Port, input.Security, input.Username, input.PasswordCiphertext, input.SenderName, input.SenderEmail, input.UpdatedBy)
	if err != nil {
		return EmailSettings{}, false, emailSettingsNameConflict(err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return EmailSettings{}, false, nil
	}
	item, _, err := s.GetEmailSettingsByID(id)
	return item, true, err
}

func (s *PostgresStore) DeleteEmailSettings(id string) (bool, bool, error) {
	result, err := s.db.Exec(`delete from smtp_configurations where id=$1 and not is_default`, id)
	if err != nil {
		return false, false, err
	}
	count, _ := result.RowsAffected()
	if count > 0 {
		return true, false, nil
	}
	var isDefault bool
	err = s.db.QueryRow(`select is_default from smtp_configurations where id=$1`, id).Scan(&isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return false, isDefault, err
}

func (s *PostgresStore) SetDefaultEmailSettings(id string) (EmailSettings, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return EmailSettings{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`lock table smtp_configurations in share row exclusive mode`); err != nil {
		return EmailSettings{}, false, err
	}
	var exists bool
	if err = tx.QueryRow(`select exists(select 1 from smtp_configurations where id=$1)`, id).Scan(&exists); err != nil || !exists {
		return EmailSettings{}, false, err
	}
	if _, err = tx.Exec(`update smtp_configurations set is_default=false where is_default and id<>$1`, id); err != nil {
		return EmailSettings{}, false, err
	}
	if _, err = tx.Exec(`update smtp_configurations set is_default=true,updated_at=now() where id=$1`, id); err != nil {
		return EmailSettings{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return EmailSettings{}, false, err
	}
	item, _, err := s.GetEmailSettingsByID(id)
	return item, true, err
}

func (s *PostgresStore) UpdateEmailSettingsTestResult(id, status, message string, testedAt time.Time) error {
	_, err := s.db.Exec(`update smtp_configurations set last_test_status=$2,last_tested_at=$3,last_test_error=$4 where id=$1`, id, status, testedAt.UTC(), message)
	return err
}
