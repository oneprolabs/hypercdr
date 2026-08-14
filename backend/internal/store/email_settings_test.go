package store

import (
	"errors"
	"testing"
	"time"
)

func smtpInput(name string) EmailSettingsInput {
	return EmailSettingsInput{Name: name, Enabled: true, Host: "smtp.example.com", Port: 587, Security: "starttls", Username: "mailer", PasswordCiphertext: "encrypted", SenderName: "HyperCDR", SenderEmail: "noreply@example.com"}
}

func TestEmailSettingsCRUDAndDefaultSelection(t *testing.T) {
	repo := NewMemoryStore()
	primary, err := repo.CreateEmailSettings(smtpInput("Primary"))
	if err != nil || !primary.IsDefault {
		t.Fatalf("first configuration should be default: item=%+v err=%v", primary, err)
	}
	secondary, err := repo.CreateEmailSettings(smtpInput("Secondary"))
	if err != nil || secondary.IsDefault {
		t.Fatalf("second configuration should not be default: item=%+v err=%v", secondary, err)
	}
	if _, err = repo.CreateEmailSettings(smtpInput("primary")); !errors.Is(err, ErrEmailSettingsNameExists) {
		t.Fatalf("case-insensitive duplicate name err=%v", err)
	}
	if deleted, isDefault, err := repo.DeleteEmailSettings(primary.ID); err != nil || deleted || !isDefault {
		t.Fatalf("default configuration must be protected: deleted=%v default=%v err=%v", deleted, isDefault, err)
	}
	selected, found, err := repo.SetDefaultEmailSettings(secondary.ID)
	if err != nil || !found || !selected.IsDefault {
		t.Fatalf("set default failed: item=%+v found=%v err=%v", selected, found, err)
	}
	current, found, err := repo.GetEmailSettings()
	if err != nil || !found || current.ID != secondary.ID {
		t.Fatalf("legacy default lookup returned %+v found=%v err=%v", current, found, err)
	}
	if deleted, isDefault, err := repo.DeleteEmailSettings(primary.ID); err != nil || !deleted || isDefault {
		t.Fatalf("old default should be deletable: deleted=%v default=%v err=%v", deleted, isDefault, err)
	}
}

func TestEmailSettingsUpdatePreservesDefaultAndTestResult(t *testing.T) {
	repo := NewMemoryStore()
	item, err := repo.CreateEmailSettings(smtpInput("Primary"))
	if err != nil {
		t.Fatal(err)
	}
	testedAt := time.Now().UTC().Truncate(time.Second)
	if err := repo.UpdateEmailSettingsTestResult(item.ID, "succeeded", "", testedAt); err != nil {
		t.Fatal(err)
	}
	input := smtpInput("Renamed")
	input.PasswordCiphertext = item.PasswordCiphertext
	updated, found, err := repo.UpdateEmailSettings(item.ID, input)
	if err != nil || !found {
		t.Fatalf("update found=%v err=%v", found, err)
	}
	if !updated.IsDefault || updated.LastTestStatus != "succeeded" || updated.LastTestedAt == nil || !updated.LastTestedAt.Equal(testedAt) {
		t.Fatalf("durable state was not preserved: %+v", updated)
	}
	items, err := repo.ListEmailSettings()
	if err != nil || len(items) != 1 || items[0].Name != "Renamed" {
		t.Fatalf("list=%+v err=%v", items, err)
	}
}
