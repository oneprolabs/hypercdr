package store

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu                  sync.Mutex
	tokens              map[string]AgentToken
	credentials         map[string]string
	clusters            map[string]Cluster
	applications        map[string]Application
	tags                map[string]Tag
	storage             map[string]StorageRepository
	bindings            map[string]ClusterStorageBinding
	policies            map[string]Policy
	plans               map[string]ProtectionPlan
	schedules           map[string]ProtectionPlanSchedule
	restorePoints       map[string]RestorePoint
	tasks               map[string]Task
	taskEvents          []TaskEvent
	diagnosticLogs      []DiagnosticLog
	logCoverage         map[string]ClusterLogCoverage
	auditLogs           []AuditLog
	users               map[string]memoryUser
	resetTokens         map[string]memoryResetToken
	platformSessions    map[string]PlatformSession
	releases            map[string]ComponentRelease
	platformReleases    map[string]PlatformRelease
	platformUpgradeJobs map[string]PlatformUpgradeJob
	platformSettings    *PlatformSettings
	emailSettings       map[string]EmailSettings
	tenants             map[string]Tenant
}

func (s *MemoryStore) GetEmailSettings() (EmailSettings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.emailSettings {
		if item.IsDefault {
			return item, true, nil
		}
	}
	return EmailSettings{}, false, nil
}
func (s *MemoryStore) UpsertEmailSettings(input EmailSettingsInput) (EmailSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, current := range s.emailSettings {
		if current.IsDefault {
			item := memoryEmailSettings(id, current.Name, true, current.CreatedAt, input)
			s.emailSettings[id] = item
			return item, nil
		}
	}
	item := memoryEmailSettings(NewPublicID(), defaultSMTPName(input.Name), true, time.Now().UTC(), input)
	s.emailSettings[item.ID] = item
	return item, nil
}

func memoryEmailSettings(id, name string, isDefault bool, createdAt time.Time, input EmailSettingsInput) EmailSettings {
	now := time.Now().UTC()
	if createdAt.IsZero() {
		createdAt = now
	}
	return EmailSettings{ID: id, Name: defaultSMTPName(name), IsDefault: isDefault, Enabled: input.Enabled, Host: input.Host, Port: input.Port, Security: input.Security, Username: input.Username, PasswordCiphertext: input.PasswordCiphertext, PasswordConfigured: input.PasswordCiphertext != "", SenderName: input.SenderName, SenderEmail: input.SenderEmail, LastTestStatus: "not_tested", CreatedAt: createdAt, UpdatedAt: now}
}

func defaultSMTPName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Default SMTP"
	}
	return strings.TrimSpace(name)
}

func (s *MemoryStore) ListEmailSettings() ([]EmailSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]EmailSettings, 0, len(s.emailSettings))
	for _, item := range s.emailSettings {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDefault != items[j].IsDefault {
			return items[i].IsDefault
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

func (s *MemoryStore) GetEmailSettingsByID(id string) (EmailSettings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.emailSettings[id]
	return item, ok, nil
}

func (s *MemoryStore) CreateEmailSettings(input EmailSettingsInput) (EmailSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.emailSettings {
		if strings.EqualFold(item.Name, strings.TrimSpace(input.Name)) {
			return EmailSettings{}, ErrEmailSettingsNameExists
		}
	}
	item := memoryEmailSettings(NewPublicID(), input.Name, len(s.emailSettings) == 0, time.Now().UTC(), input)
	s.emailSettings[item.ID] = item
	return item, nil
}

func (s *MemoryStore) UpdateEmailSettings(id string, input EmailSettingsInput) (EmailSettings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.emailSettings[id]
	if !ok {
		return EmailSettings{}, false, nil
	}
	for otherID, item := range s.emailSettings {
		if otherID != id && strings.EqualFold(item.Name, strings.TrimSpace(input.Name)) {
			return EmailSettings{}, true, ErrEmailSettingsNameExists
		}
	}
	updated := memoryEmailSettings(id, input.Name, current.IsDefault, current.CreatedAt, input)
	updated.LastTestStatus, updated.LastTestedAt, updated.LastTestError = current.LastTestStatus, current.LastTestedAt, current.LastTestError
	s.emailSettings[id] = updated
	return updated, true, nil
}

func (s *MemoryStore) DeleteEmailSettings(id string) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.emailSettings[id]
	if !ok {
		return false, false, nil
	}
	if item.IsDefault {
		return false, true, nil
	}
	delete(s.emailSettings, id)
	return true, false, nil
}

func (s *MemoryStore) SetDefaultEmailSettings(id string) (EmailSettings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	selected, ok := s.emailSettings[id]
	if !ok {
		return EmailSettings{}, false, nil
	}
	for itemID, item := range s.emailSettings {
		item.IsDefault = itemID == id
		s.emailSettings[itemID] = item
	}
	selected.IsDefault = true
	return selected, true, nil
}

func (s *MemoryStore) UpdateEmailSettingsTestResult(id, status, message string, testedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.emailSettings[id]
	if !ok {
		return nil
	}
	item.LastTestStatus, item.LastTestedAt, item.LastTestError = status, &testedAt, message
	s.emailSettings[id] = item
	return nil
}

func (s *MemoryStore) GetPlatformSettings() (PlatformSettings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.platformSettings == nil {
		return PlatformSettings{}, false, nil
	}
	return *s.platformSettings, true, nil
}

func (s *MemoryStore) UpsertPlatformSettings(input PlatformSettingsInput) (PlatformSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	createdAt := now
	if s.platformSettings != nil {
		createdAt = s.platformSettings.CreatedAt
	}
	item := PlatformSettings{TenantID: DefaultTenantID, ImageRegistry: strings.TrimRight(strings.TrimSpace(input.ImageRegistry), "/"), AgentNamespace: input.AgentNamespace, VeleroVersion: input.VeleroVersion, PublicEndpoint: strings.TrimRight(strings.TrimSpace(input.PublicEndpoint), "/"), CreatedAt: createdAt, UpdatedAt: now}
	s.platformSettings = &item
	return item, nil
}

type memoryUser struct {
	User
	Password string
}
type memoryResetToken struct {
	Email     string
	ExpiresAt time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tokens:              map[string]AgentToken{},
		credentials:         map[string]string{},
		clusters:            map[string]Cluster{},
		applications:        map[string]Application{},
		tags:                map[string]Tag{},
		storage:             map[string]StorageRepository{},
		bindings:            map[string]ClusterStorageBinding{},
		policies:            map[string]Policy{},
		plans:               map[string]ProtectionPlan{},
		schedules:           map[string]ProtectionPlanSchedule{},
		restorePoints:       map[string]RestorePoint{},
		tasks:               map[string]Task{},
		taskEvents:          []TaskEvent{},
		logCoverage:         map[string]ClusterLogCoverage{},
		auditLogs:           []AuditLog{},
		tenants:             map[string]Tenant{DefaultTenantID: {ID: DefaultTenantID, Name: "Admin", Status: "active", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}},
		users:               map[string]memoryUser{DefaultAdminEmail: {User: User{ID: "00000000-0000-0000-0000-00000000a001", TenantID: DefaultTenantID, Email: DefaultAdminEmail, Role: "admin", Status: "active", AuthProvider: "password", SystemAdmin: true, MustChangePassword: true}, Password: DefaultAdminPassword}},
		resetTokens:         map[string]memoryResetToken{},
		platformSessions:    map[string]PlatformSession{},
		releases:            map[string]ComponentRelease{},
		platformReleases:    map[string]PlatformRelease{},
		platformUpgradeJobs: map[string]PlatformUpgradeJob{},
		emailSettings:       map[string]EmailSettings{},
	}
}

func (s *MemoryStore) ListTenants() ([]Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Tenant, 0, len(s.tenants))
	for _, tenant := range s.tenants {
		tenant.UserCount, tenant.ClusterCount = 0, 0
		for _, user := range s.users {
			if user.TenantID == tenant.ID {
				tenant.UserCount++
			}
		}
		for _, cluster := range s.clusters {
			if cluster.TenantID == tenant.ID {
				tenant.ClusterCount++
			}
		}
		items = append(items, tenant)
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	return items, nil
}

func (s *MemoryStore) GetTenant(id string) (Tenant, bool, error) {
	items, _ := s.ListTenants()
	for _, item := range items {
		if item.ID == id {
			return item, true, nil
		}
	}
	return Tenant{}, false, nil
}

func (s *MemoryStore) CreateTenant(input TenantInput) (Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(input.Name)
	if len(strings.TrimSpace(input.Description)) > 500 {
		return Tenant{}, errors.New("tenant description must not exceed 500 characters")
	}
	for _, item := range s.tenants {
		if strings.EqualFold(item.Name, name) {
			return Tenant{}, errors.New("tenant already exists")
		}
	}
	status := input.Status
	if status != "disabled" {
		status = "active"
	}
	now := time.Now().UTC()
	item := Tenant{ID: newID(), Name: name, Description: strings.TrimSpace(input.Description), Status: status, CreatedAt: now, UpdatedAt: now}
	s.tenants[item.ID] = item
	return item, nil
}

func (s *MemoryStore) UpdateTenant(id string, input TenantInput) (Tenant, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.tenants[id]
	if !ok {
		return Tenant{}, false, nil
	}
	if len(strings.TrimSpace(input.Description)) > 500 {
		return Tenant{}, false, errors.New("tenant description must not exceed 500 characters")
	}
	name := strings.TrimSpace(input.Name)
	for otherID, other := range s.tenants {
		if otherID != id && strings.EqualFold(other.Name, name) {
			return Tenant{}, false, errors.New("tenant already exists")
		}
	}
	item.Name = name
	item.Description = strings.TrimSpace(input.Description)
	if input.Status == "disabled" {
		item.Status = "disabled"
	} else {
		item.Status = "active"
	}
	item.UpdatedAt = time.Now().UTC()
	s.tenants[id] = item
	return item, true, nil
}

func (s *MemoryStore) DeleteTenant(id string) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == DefaultTenantID {
		return false, true, nil
	}
	if _, ok := s.tenants[id]; !ok {
		return false, false, nil
	}
	for _, user := range s.users {
		if user.TenantID == id {
			return false, true, nil
		}
	}
	for _, cluster := range s.clusters {
		if cluster.TenantID == id {
			return false, true, nil
		}
	}
	for _, item := range s.storage {
		if item.TenantID == id {
			return false, true, nil
		}
	}
	for _, item := range s.policies {
		if item.TenantID == id {
			return false, true, nil
		}
	}
	for _, item := range s.plans {
		if item.TenantID == id {
			return false, true, nil
		}
	}
	delete(s.tenants, id)
	return true, false, nil
}

func (s *MemoryStore) CreateAuditLog(input AuditLogInput) (AuditLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenantID := DefaultTenantID
	for _, user := range s.users {
		if user.ID == input.ActorID {
			tenantID = user.TenantID
			break
		}
	}
	item := AuditLog{ID: newID(), TenantID: tenantID, ActorID: input.ActorID, Actor: input.Actor, Action: input.Action, ResourceType: input.ResourceType, ResourceID: input.ResourceID, ResourceName: input.ResourceName, Result: input.Result, Message: input.Message, Payload: input.Payload, CreatedAt: time.Now().UTC()}
	if item.Payload == nil {
		item.Payload = map[string]any{}
	}
	s.auditLogs = append(s.auditLogs, item)
	return item, nil
}

func (s *MemoryStore) ListAuditLogs(limit, offset int) ([]AuditLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]AuditLog, 0, len(s.auditLogs))
	for _, item := range s.auditLogs {
		if strings.TrimSpace(item.ActorID) != "" && item.Action != "Create Cluster Registration Token" && item.Action != "Start Cluster Registration" {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if offset >= len(items) {
		return []AuditLog{}, nil
	}
	if offset < 0 {
		offset = 0
	}
	end := len(items)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return items[offset:end], nil
}

func (s *MemoryStore) ListComponentReleases(component string) ([]ComponentRelease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]ComponentRelease, 0)
	for _, item := range s.releases {
		if component == "" || item.Component == component {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *MemoryStore) GetActiveComponentRelease(component string) (ComponentRelease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.releases {
		if item.Component == component && item.Status == "active" {
			return item, true, nil
		}
	}
	return ComponentRelease{}, false, nil
}

func (s *MemoryStore) UpsertComponentRelease(input ComponentReleaseInput) (ComponentRelease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, item := range s.releases {
		if item.Component == input.Component && item.ImageDigest == input.ImageDigest {
			item.Version, item.Image, item.ReleaseNotes, item.UpdatedAt = input.Version, input.Image, input.ReleaseNotes, now
			s.releases[id] = item
			return item, nil
		}
	}
	status := input.Status
	if status == "" {
		status = "candidate"
	}
	item := ComponentRelease{ID: newID(), TenantID: DefaultTenantID, Component: input.Component, Version: input.Version, Image: input.Image, ImageDigest: input.ImageDigest, Status: status, ReleaseNotes: input.ReleaseNotes, PublishedBy: input.PublishedBy, CreatedAt: now, UpdatedAt: now}
	if status == "active" {
		item.PublishedAt = now
	}
	s.releases[item.ID] = item
	return item, nil
}

func (s *MemoryStore) ActivateComponentRelease(id string, publishedBy string) (ComponentRelease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, ok := s.releases[id]
	if !ok {
		return ComponentRelease{}, false, nil
	}
	now := time.Now().UTC()
	for key, item := range s.releases {
		if item.Component == target.Component && item.Status == "active" {
			item.Status, item.UpdatedAt = "retired", now
			s.releases[key] = item
		}
	}
	target.Status, target.PublishedBy, target.PublishedAt, target.UpdatedAt = "active", publishedBy, now, now
	s.releases[id] = target
	return target, true, nil
}

func (s *MemoryStore) ListPlatformReleases() ([]PlatformRelease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []PlatformRelease{}
	for _, v := range s.platformReleases {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}
func (s *MemoryStore) UpsertPlatformRelease(input PlatformReleaseInput) (PlatformRelease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, v := range s.platformReleases {
		if v.Version == input.Version {
			v.APIImage = input.APIImage
			v.APIImageDigest = input.APIImageDigest
			v.FrontendImage = input.FrontendImage
			v.FrontendImageDigest = input.FrontendImageDigest
			v.DatabaseSchemaVersion = input.DatabaseSchemaVersion
			v.MinimumAgentVersion = input.MinimumAgentVersion
			v.RollbackSupported = input.RollbackSupported
			v.ReleaseNotes = input.ReleaseNotes
			v.UpdatedAt = now
			s.platformReleases[id] = v
			return v, nil
		}
	}
	status := input.Status
	if status == "" {
		status = "candidate"
	}
	v := PlatformRelease{ID: newID(), TenantID: DefaultTenantID, Version: input.Version, APIImage: input.APIImage, APIImageDigest: input.APIImageDigest, FrontendImage: input.FrontendImage, FrontendImageDigest: input.FrontendImageDigest, DatabaseSchemaVersion: input.DatabaseSchemaVersion, MinimumAgentVersion: input.MinimumAgentVersion, RollbackSupported: input.RollbackSupported, ReleaseNotes: input.ReleaseNotes, Status: status, PublishedBy: input.PublishedBy, CreatedAt: now, UpdatedAt: now}
	if status == "active" {
		v.PublishedAt = now
	}
	s.platformReleases[v.ID] = v
	return v, nil
}
func (s *MemoryStore) ActivatePlatformRelease(id, publishedBy string) (PlatformRelease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, ok := s.platformReleases[id]
	if !ok {
		return PlatformRelease{}, false, nil
	}
	now := time.Now().UTC()
	for k, v := range s.platformReleases {
		if v.Status == "active" {
			v.Status = "retired"
			v.UpdatedAt = now
			s.platformReleases[k] = v
		}
	}
	target.Status = "active"
	target.PublishedBy = publishedBy
	target.PublishedAt = now
	target.UpdatedAt = now
	s.platformReleases[id] = target
	return target, true, nil
}
func (s *MemoryStore) ListPlatformUpgradeJobs() ([]PlatformUpgradeJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []PlatformUpgradeJob{}
	for _, v := range s.platformUpgradeJobs {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}
func (s *MemoryStore) CreatePlatformUpgradeJob(input PlatformUpgradeJobInput) (PlatformUpgradeJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.platformUpgradeJobs {
		if !isTerminalPlatformUpgradeStatus(v.Status) {
			return PlatformUpgradeJob{}, errors.New("platform upgrade already active")
		}
	}
	now := time.Now().UTC()
	r := input.Release
	j := PlatformUpgradeJob{ID: newID(), TenantID: DefaultTenantID, ReleaseID: r.ID, FromVersion: input.FromVersion, TargetVersion: r.Version, Status: "queued", Step: "queued", APIImage: r.APIImage, APIImageDigest: r.APIImageDigest, FrontendImage: r.FrontendImage, FrontendImageDigest: r.FrontendImageDigest, DatabaseSchemaVersion: r.DatabaseSchemaVersion, RollbackSupported: r.RollbackSupported, PreviousAPIImage: input.PreviousAPIImage, PreviousFrontendImage: input.PreviousFrontendImage, RequestedBy: input.RequestedBy, CreatedAt: now, UpdatedAt: now}
	s.platformUpgradeJobs[j.ID] = j
	return j, nil
}
func (s *MemoryStore) UpdatePlatformUpgradeJob(input PlatformUpgradeJobUpdate) (PlatformUpgradeJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.platformUpgradeJobs[input.ID]
	if !ok {
		return PlatformUpgradeJob{}, false, nil
	}
	now := time.Now().UTC()
	if input.Status != "" {
		j.Status = input.Status
	}
	if input.Step != "" {
		j.Step = input.Step
	}
	if input.Progress >= 0 {
		j.Progress = input.Progress
	}
	if input.BackupPath != "" {
		j.BackupPath = input.BackupPath
	}
	if input.PreviousAPIImage != "" {
		j.PreviousAPIImage = input.PreviousAPIImage
	}
	if input.PreviousFrontendImage != "" {
		j.PreviousFrontendImage = input.PreviousFrontendImage
	}
	j.ErrorCode = input.ErrorCode
	j.ErrorMessage = input.ErrorMessage
	if input.ExecutorID != "" {
		j.ExecutorID = input.ExecutorID
		j.ExecutorHeartbeatAt = now
	}
	if input.MarkStarted && j.StartedAt.IsZero() {
		j.StartedAt = now
	}
	if input.MarkDone {
		j.CompletedAt = now
	}
	j.UpdatedAt = now
	s.platformUpgradeJobs[j.ID] = j
	return j, true, nil
}
func isTerminalPlatformUpgradeStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled" || status == "rolled_back"
}

func (s *MemoryStore) AuthenticateUser(input UserAuthInput) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[strings.ToLower(strings.TrimSpace(input.Email))]
	if !ok || u.Password != input.Password {
		return User{}, false, nil
	}
	if !u.SystemAdmin && s.tenants[u.TenantID].Status != "active" {
		return User{}, false, nil
	}
	return u.User, true, nil
}

func (s *MemoryStore) CreateUser(tenantID, email, password string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	if _, ok := s.users[email]; ok {
		return User{}, ErrUserExists
	}
	if tenant, ok := s.tenants[tenantID]; !ok || tenant.Status != "active" {
		return User{}, errors.New("tenant is not active")
	}
	u := User{ID: newID(), TenantID: tenantID, Email: email, Role: "operator", Status: "active", AuthProvider: "password", MustChangePassword: true}
	s.users[email] = memoryUser{User: u, Password: password}
	return u, nil
}

func (s *MemoryStore) ListUsers() ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]User, 0, len(s.users))
	for _, item := range s.users {
		user := item.User
		user.TenantName = s.tenants[user.TenantID].Name
		out = append(out, user)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}
func (s *MemoryStore) GetUser(id string) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.users {
		if item.ID == id {
			user := item.User
			user.TenantName = s.tenants[user.TenantID].Name
			return user, true, nil
		}
	}
	return User{}, false, nil
}
func (s *MemoryStore) UpdateUser(input UserUpdateInput) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, item := range s.users {
		if item.ID != input.ID {
			continue
		}
		delete(s.users, key)
		if !item.SystemAdmin {
			item.Email = strings.ToLower(strings.TrimSpace(input.Email))
		}
		item.DisplayName = strings.TrimSpace(input.DisplayName)
		if !item.SystemAdmin && input.TenantID != "" {
			item.TenantID = input.TenantID
		}
		if !item.SystemAdmin {
			item.Role = input.Role
			item.Status = input.Status
		}
		item.TimeZone = strings.TrimSpace(input.TimeZone)
		s.users[item.Email] = item
		user := item.User
		user.TenantName = s.tenants[user.TenantID].Name
		return user, true, nil
	}
	return User{}, false, nil
}
func (s *MemoryStore) DeleteUser(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, item := range s.users {
		if item.ID == id && item.Email != DefaultAdminEmail {
			delete(s.users, key)
			return true, nil
		}
	}
	return false, nil
}
func (s *MemoryStore) SetUserPassword(id, password string, mustChangePassword bool) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, item := range s.users {
		if item.ID == id {
			item.Password = password
			item.MustChangePassword = mustChangePassword
			s.users[key] = item
			for token, session := range s.platformSessions {
				if session.UserID == id {
					delete(s.platformSessions, token)
				}
			}
			return item.User, true, nil
		}
	}
	return User{}, false, nil
}
func (s *MemoryStore) CreatePlatformSession(userID string, ttl time.Duration) (PlatformSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := "hcs_" + newID() + newID()
	session := PlatformSession{Token: token, UserID: userID, ExpiresAt: time.Now().UTC().Add(ttl)}
	s.platformSessions[token] = session
	return session, nil
}
func (s *MemoryStore) AuthenticatePlatformSession(token string) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.platformSessions[token]
	if !ok || time.Now().UTC().After(session.ExpiresAt) {
		return User{}, false, nil
	}
	for _, item := range s.users {
		if item.ID == session.UserID && item.Status == "active" {
			if !item.SystemAdmin && s.tenants[item.TenantID].Status != "active" {
				return User{}, false, nil
			}
			return item.User, true, nil
		}
	}
	return User{}, false, nil
}
func (s *MemoryStore) DeletePlatformSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.platformSessions, token)
	return nil
}

func (s *MemoryStore) CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	user, ok := s.users[email]
	if !ok || user.SystemAdmin || s.tenants[user.TenantID].Status != "active" {
		return "", false, nil
	}
	token := "hpr_" + newID() + newID()
	s.resetTokens[token] = memoryResetToken{Email: email, ExpiresAt: time.Now().UTC().Add(ttl)}
	return token, true, nil
}

func (s *MemoryStore) ResetPassword(token, password string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.resetTokens[token]
	if !ok || time.Now().UTC().After(r.ExpiresAt) {
		return User{}, ErrResetInvalid
	}
	delete(s.resetTokens, token)
	u := s.users[r.Email]
	u.Password = password
	u.MustChangePassword = false
	s.users[r.Email] = u
	return u.User, nil
}

func (s *MemoryStore) FindOrCreateGoogleUser(email string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	if u, ok := s.users[email]; ok {
		return u.User, nil
	}
	u := User{ID: newID(), TenantID: DefaultTenantID, Email: email, Role: "operator", Status: "active", AuthProvider: "google"}
	s.users[email] = memoryUser{User: u}
	return u, nil
}

func (s *MemoryStore) CreateAgentToken(tenantID, createdBy, description string, ttl time.Duration) (AgentToken, error) {
	now := time.Now().UTC()
	token := AgentToken{
		ID:          newID(),
		TenantID:    tenantID,
		CreatedBy:   createdBy,
		Token:       "hcdr_" + newID() + newID(),
		Description: description,
		ExpiresAt:   now.Add(ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token.Token] = token
	return token, nil
}

func (s *MemoryStore) ValidateAgentToken(value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.tokens[value]
	if !ok {
		return ErrTokenInvalid
	}
	if !token.UsedAt.IsZero() {
		return ErrTokenUsed
	}
	if time.Now().UTC().After(token.ExpiresAt) {
		return ErrTokenExpired
	}
	return nil
}

func (s *MemoryStore) RegisterCluster(input RegisterClusterInput) (Cluster, string, error) {
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokens[input.Token]
	if !ok {
		return Cluster{}, "", ErrTokenInvalid
	}
	if !token.UsedAt.IsZero() {
		return Cluster{}, "", ErrTokenUsed
	}
	if now.After(token.ExpiresAt) {
		return Cluster{}, "", ErrTokenExpired
	}

	clusterName := input.ClusterName
	if clusterName == "" {
		clusterName = "registered-cluster"
	}

	isFirstCluster := true
	for _, existing := range s.clusters {
		if existing.TenantID == token.TenantID {
			isFirstCluster = false
			break
		}
	}
	cluster := Cluster{
		ID:               newID(),
		TenantID:         token.TenantID,
		Name:             clusterName,
		KubeVersion:      input.KubeVersion,
		Status:           "healthy",
		ConnectionStatus: "online",
		AgentVersion:     input.AgentVersion,
		VeleroVersion:    input.VeleroVersion,
		VeleroStatus:     input.VeleroStatus,
		Role:             "both",
		IsDefault:        isFirstCluster,
		RegisteredAt:     now,
		LastSeenAt:       now,
	}

	token.UsedAt = now
	token.ClusterID = cluster.ID
	s.tokens[input.Token] = token
	s.clusters[cluster.ID] = cluster

	credential := "cred_" + newID() + newID()
	s.credentials[cluster.ID] = credential
	return cluster, credential, nil
}

func (s *MemoryStore) AuthenticateAgentCredential(input AgentCredentialInput) (Cluster, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	expected, ok := s.credentials[input.ClusterID]
	if !ok || expected == "" || expected != input.Credential {
		return Cluster{}, false, nil
	}
	cluster, ok := s.clusters[input.ClusterID]
	if !ok {
		return Cluster{}, false, nil
	}
	cluster.ConnectionStatus = "online"
	cluster.LastSeenAt = time.Now().UTC()
	s.clusters[input.ClusterID] = cluster
	return cluster, true, nil
}

func (s *MemoryStore) ListClusters() ([]Cluster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clusters := make([]Cluster, 0, len(s.clusters))
	for _, cluster := range s.clusters {
		applyClusterConnectionFreshness(&cluster)
		clusters = append(clusters, cluster)
	}
	return clusters, nil
}

func (s *MemoryStore) UpdateCluster(input ClusterUpdateInput) (Cluster, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, ok := s.clusters[input.ID]
	if !ok {
		return Cluster{}, false, nil
	}
	if input.Name != "" {
		cluster.Name = input.Name
	}
	if input.Role != "" {
		cluster.Role = normalizeClusterRole(input.Role)
	}
	if cluster.Role == "" {
		cluster.Role = "both"
	}
	if input.IsDefault != nil {
		if *input.IsDefault {
			for id, item := range s.clusters {
				if item.TenantID == cluster.TenantID {
					item.IsDefault = false
					s.clusters[id] = item
				}
			}
		}
		cluster.IsDefault = *input.IsDefault
	}
	s.clusters[input.ID] = cluster
	return cluster, true, nil
}

func (s *MemoryStore) SetClusterConnectionStatus(clusterID string, status string) (Cluster, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, ok := s.clusters[clusterID]
	if !ok {
		return Cluster{}, false, nil
	}
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "offline"
	}
	cluster.ConnectionStatus = status
	s.clusters[clusterID] = cluster
	return cluster, true, nil
}

func (s *MemoryStore) SetDefaultCluster(clusterID string) (Cluster, bool, error) {
	value := true
	return s.UpdateCluster(ClusterUpdateInput{ID: clusterID, IsDefault: &value})
}

func (s *MemoryStore) DeleteCluster(clusterID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed, ok := s.clusters[clusterID]
	if !ok {
		return false, nil
	}
	delete(s.clusters, clusterID)
	if removed.IsDefault {
		var nextID string
		var nextRegisteredAt time.Time
		for id, cluster := range s.clusters {
			if cluster.TenantID != removed.TenantID {
				continue
			}
			if nextID == "" || cluster.RegisteredAt.Before(nextRegisteredAt) || (cluster.RegisteredAt.Equal(nextRegisteredAt) && id < nextID) {
				nextID = id
				nextRegisteredAt = cluster.RegisteredAt
			}
		}
		if nextID != "" {
			next := s.clusters[nextID]
			next.IsDefault = true
			s.clusters[nextID] = next
		}
	}
	delete(s.credentials, clusterID)
	for tokenKey, token := range s.tokens {
		if token.ClusterID == clusterID {
			token.ClusterID = ""
			s.tokens[tokenKey] = token
		}
	}
	for key, app := range s.applications {
		if app.ClusterID == clusterID {
			delete(s.applications, key)
		}
	}
	for planID, plan := range s.plans {
		if plan.SourceClusterID == clusterID || plan.TargetClusterID == clusterID {
			delete(s.plans, planID)
		}
	}
	for pointID, point := range s.restorePoints {
		if point.SourceClusterID == clusterID {
			delete(s.restorePoints, pointID)
		}
	}
	for taskID, task := range s.tasks {
		if task.ClusterID == clusterID {
			if task.Payload == nil {
				task.Payload = map[string]any{}
			}
			task.Payload["archivedClusterId"] = clusterID
			task.ClusterID = ""
			s.tasks[taskID] = task
		}
	}
	return true, nil
}

func (s *MemoryStore) ListApplications(clusterID string) ([]Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make([]Application, 0)
	for _, app := range s.applications {
		if clusterID == "" || app.ClusterID == clusterID {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

func (s *MemoryStore) UpdateApplication(input ApplicationUpdateInput) (Application, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	protection := strings.TrimSpace(input.ProtectionStatus)
	if protection == "" {
		protection = "unprotected"
	}
	switch protection {
	case "unprotected", "pending_protection", "protected":
	default:
		return Application{}, false, errors.New("invalid_protection_status")
	}
	app, ok := s.applications[input.ID]
	if !ok {
		return Application{}, false, nil
	}
	app.ProtectionStatus = protection
	s.applications[input.ID] = app
	return app, true, nil
}

func (s *MemoryStore) ListTags() ([]Tag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Tag, 0, len(s.tags))
	for _, tag := range s.tags {
		items = append(items, tag)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}
func (s *MemoryStore) CreateTag(tenantID, name string) (Tag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	now := time.Now().UTC()
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	for _, tag := range s.tags {
		if tag.TenantID == tenantID && strings.EqualFold(tag.Name, name) {
			return Tag{}, ErrUserExists
		}
	}
	tag := Tag{ID: newID(), TenantID: tenantID, Name: name, CreatedAt: now, UpdatedAt: now}
	s.tags[tag.ID] = tag
	return tag, nil
}
func (s *MemoryStore) UpdateTag(id, name string) (Tag, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tag, ok := s.tags[id]
	if !ok {
		return Tag{}, false, nil
	}
	tag.Name = strings.TrimSpace(name)
	tag.UpdatedAt = time.Now().UTC()
	s.tags[id] = tag
	return tag, true, nil
}
func (s *MemoryStore) DeleteTag(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tags[id]; !ok {
		return false, nil
	}
	delete(s.tags, id)
	for key, app := range s.applications {
		app.Tags = removeString(app.Tags, id)
		s.applications[key] = app
	}
	return true, nil
}
func (s *MemoryStore) SetApplicationTags(applicationID string, tagIDs []string) (Application, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, ok := s.applications[applicationID]
	if !ok {
		return Application{}, false, nil
	}
	cluster, clusterOK := s.clusters[app.ClusterID]
	if !clusterOK {
		return Application{}, false, nil
	}
	app.Tags = app.Tags[:0]
	for _, tagID := range tagIDs {
		if tag, ok := s.tags[tagID]; ok && tag.TenantID == cluster.TenantID {
			app.Tags = append(app.Tags, tagID)
		}
	}
	s.applications[applicationID] = app
	return app, true, nil
}
func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func (s *MemoryStore) ApplyInventory(input InventoryInput) (Cluster, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, ok := s.clusters[input.ClusterID]
	if !ok {
		return Cluster{}, false, nil
	}

	cluster.KubeVersion = input.KubeVersion
	if input.VeleroStatus != "" {
		cluster.VeleroStatus = input.VeleroStatus
	}
	cluster.NodeCount = input.NodeCount
	cluster.NamespaceCount = input.NamespaceCount
	cluster.ApplicationCount = len(input.Apps)
	cluster.InventoryHash = input.Hash
	cluster.Nodes = input.Nodes
	cluster.StorageClasses = input.StorageClasses
	if input.CapabilityScan {
		cluster.APIResources = input.APIResources
		cluster.NamespaceAPIs = mergeNamespaceAPIs(cluster.NamespaceAPIs, input.NamespaceAPIs, input.CapabilityNamespace)
		cluster.Capabilities = input.Capabilities
		cluster.CapabilitiesCollectedAt = input.CollectedAt
		cluster.CapabilitiesComplete = input.CapabilitiesComplete
	}
	cluster.LastSeenAt = time.Now().UTC()
	cluster.ConnectionStatus = "online"
	s.clusters[input.ClusterID] = cluster

	for key, app := range s.applications {
		if app.ClusterID == input.ClusterID {
			delete(s.applications, key)
		}
	}

	for _, app := range input.Apps {
		if app.ID == "" {
			app.ID = newID()
		}
		app.ClusterID = input.ClusterID
		if app.Name == "" {
			app.Name = app.Namespace
		}
		if app.LastCollectedAt.IsZero() {
			app.LastCollectedAt = input.CollectedAt
		}
		key := input.ClusterID + "/" + app.Namespace
		s.applications[key] = app
	}

	return cluster, true, nil
}

func (s *MemoryStore) UpdateHeartbeat(input HeartbeatInput) (Cluster, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cluster, ok := s.clusters[input.ClusterID]
	if !ok {
		return Cluster{}, false, nil
	}

	cluster.ConnectionStatus = "online"
	cluster.LastSeenAt = time.Now().UTC()
	if input.Status != "" {
		cluster.Status = input.Status
	}
	if input.KubeVersion != "" {
		cluster.KubeVersion = input.KubeVersion
	}
	if input.AgentVersion != "" {
		cluster.AgentVersion = input.AgentVersion
	}
	if input.AgentImage != "" {
		cluster.AgentImage = input.AgentImage
	}
	if input.AgentImageID != "" {
		cluster.AgentImageID = input.AgentImageID
	}
	if input.AgentImageDigest != "" {
		cluster.AgentImageDigest = input.AgentImageDigest
	}
	if input.VeleroStatus != "" {
		cluster.VeleroStatus = input.VeleroStatus
	}
	if input.VeleroVersion != "" {
		cluster.VeleroVersion = input.VeleroVersion
	}
	if input.VeleroImage != "" {
		cluster.VeleroImage = input.VeleroImage
	}
	if input.VeleroImageDigest != "" {
		cluster.VeleroImageDigest = input.VeleroImageDigest
	}
	cluster.VeleroServerReady = input.VeleroServerReady
	cluster.VeleroNodeAgentDesired = input.VeleroNodeAgentDesired
	cluster.VeleroNodeAgentReady = input.VeleroNodeAgentReady
	if input.VeleroNodeAgentImageDigest != "" {
		cluster.VeleroNodeAgentImageDigest = input.VeleroNodeAgentImageDigest
	}
	if input.NodeCount > 0 {
		cluster.NodeCount = input.NodeCount
	}
	if input.NamespaceCount > 0 {
		cluster.NamespaceCount = input.NamespaceCount
	}
	if input.ApplicationCount > 0 {
		cluster.ApplicationCount = input.ApplicationCount
	}
	cluster.ActiveTasks = input.ActiveTasks
	if input.InventoryHash != "" {
		cluster.InventoryHash = input.InventoryHash
	}

	s.clusters[input.ClusterID] = cluster
	return cluster, true, nil
}

func (s *MemoryStore) CreateStorageRepository(input StorageRepositoryInput) (StorageRepository, error) {
	if input.TenantID == "" {
		input.TenantID = DefaultTenantID
	}
	input.Region = normalizeStorageRegionValue(input.Region)
	now := time.Now().UTC()
	if input.Type == "" {
		input.Type = "S3"
	}
	repoID := newID()
	secretRef := input.SecretRef
	if secretRef == "" {
		secretRef = storageSecretName(repoID)
	}
	secret := storageSecretPayload(input)
	repo := StorageRepository{
		ID:         repoID,
		TenantID:   input.TenantID,
		Name:       input.Name,
		Type:       input.Type,
		Endpoint:   input.Endpoint,
		Bucket:     input.Bucket,
		Region:     input.Region,
		TLSEnabled: input.TLSEnabled,
		Status:     "unknown",
		Config:     input.Config,
		SecretRef:  secretRef,
		Secret:     secret,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if repo.Config == nil {
		repo.Config = map[string]any{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storage[repo.ID] = repo
	return repo, nil
}

func (s *MemoryStore) ListStorageRepositories() ([]StorageRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]StorageRepository, 0, len(s.storage))
	for _, item := range s.storage {
		items = append(items, item)
	}
	return items, nil
}

func (s *MemoryStore) UpdateStorageRepository(id string, input StorageRepositoryInput) (StorageRepository, bool, error) {
	input.Region = normalizeStorageRegionValue(input.Region)
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.storage[id]
	if !ok {
		return StorageRepository{}, false, nil
	}
	item.Name, item.Type, item.Endpoint, item.Bucket, item.Region, item.TLSEnabled = input.Name, input.Type, input.Endpoint, input.Bucket, input.Region, input.TLSEnabled
	item.Config = input.Config
	if item.Config == nil {
		item.Config = map[string]any{}
	}
	if secret := storageSecretPayload(input); len(secret) > 0 {
		item.Secret = secret
	}
	item.Status = "unknown"
	item.LastValidatedAt = time.Time{}
	item.UpdatedAt = time.Now().UTC()
	s.storage[id] = item
	return item, true, nil
}

func (s *MemoryStore) DeleteStorageRepository(id string) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.storage[id]; !ok {
		return false, false, nil
	}
	for _, plan := range s.plans {
		if plan.StorageRepoID == id {
			return false, true, nil
		}
	}
	delete(s.storage, id)
	return true, false, nil
}

func (s *MemoryStore) SetStorageRepositoryStatus(id string, status string, lastValidatedAt time.Time) (StorageRepository, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.storage[id]
	if !ok {
		return StorageRepository{}, false, nil
	}
	if status == "" {
		status = "unknown"
	}
	item.Status = status
	if !lastValidatedAt.IsZero() {
		item.LastValidatedAt = lastValidatedAt
	}
	item.UpdatedAt = time.Now().UTC()
	s.storage[id] = item
	return item, true, nil
}

func (s *MemoryStore) GetStorageRepository(id string) (StorageRepository, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.storage[id]
	return item, ok, nil
}

func (s *MemoryStore) UpsertClusterStorageBinding(input ClusterStorageBindingInput) (ClusterStorageBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	cluster, clusterOK := s.clusters[input.ClusterID]
	repository, repositoryOK := s.storage[input.StorageRepoID]
	if !clusterOK || !repositoryOK || cluster.TenantID != repository.TenantID {
		return ClusterStorageBinding{}, errors.New("cluster and storage repository must belong to the same tenant")
	}
	sourceClusterID := input.SourceClusterID
	if sourceClusterID == "" {
		sourceClusterID = input.ClusterID
	}
	key := input.ClusterID + ":" + input.StorageRepoID + ":" + sourceClusterID
	item, ok := s.bindings[key]
	if !ok {
		item = ClusterStorageBinding{
			ID:              newID(),
			TenantID:        cluster.TenantID,
			ClusterID:       input.ClusterID,
			StorageRepoID:   input.StorageRepoID,
			SourceClusterID: sourceClusterID,
			CreatedAt:       now,
		}
	}
	item.BSLName = input.BSLName
	if item.BSLName == "" {
		item.BSLName = "default"
	}
	item.SourceClusterID = sourceClusterID
	item.ObjectPrefix = input.ObjectPrefix
	item.Status = input.Status
	if item.Status == "" {
		item.Status = "pending"
	}
	item.RetryCount = input.RetryCount
	item.RepoUpdatedAt = input.RepoUpdatedAt
	item.UpdatedAt = now
	s.bindings[key] = item
	return item, nil
}

func (s *MemoryStore) GetClusterStorageBinding(clusterID string, storageRepoID string, sourceClusterID string) (ClusterStorageBinding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sourceClusterID == "" {
		sourceClusterID = clusterID
	}
	item, ok := s.bindings[clusterID+":"+storageRepoID+":"+sourceClusterID]
	return item, ok, nil
}

func (s *MemoryStore) UpdateClusterStorageBindingStatus(input ClusterStorageBindingStatusInput) (ClusterStorageBinding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sourceClusterID := input.SourceClusterID
	if sourceClusterID == "" {
		sourceClusterID = input.ClusterID
	}
	key := input.ClusterID + ":" + input.StorageRepoID + ":" + sourceClusterID
	item, ok := s.bindings[key]
	if !ok {
		return ClusterStorageBinding{}, false, nil
	}
	if input.Status != "" {
		item.Status = input.Status
	}
	if input.RetryCount > item.RetryCount {
		item.RetryCount = input.RetryCount
	}
	if !input.LastSyncedAt.IsZero() {
		item.LastSyncedAt = input.LastSyncedAt
	}
	if !input.LastSuccessAt.IsZero() {
		item.LastSuccessAt = input.LastSuccessAt
	}
	item.LastErrorCode = input.LastErrorCode
	item.LastErrorMessage = input.LastErrorMessage
	if !input.RepoUpdatedAt.IsZero() {
		item.RepoUpdatedAt = input.RepoUpdatedAt
	}
	item.UpdatedAt = time.Now().UTC()
	s.bindings[key] = item
	return item, true, nil
}

func (s *MemoryStore) CreatePolicy(input PolicyInput) (Policy, error) {
	if input.TenantID == "" {
		input.TenantID = DefaultTenantID
	}
	now := time.Now().UTC()
	if input.Composition == "" {
		input.Composition = "manual"
	}
	if input.ScheduleType == "" {
		input.ScheduleType = "manual"
	}
	if input.Status == "" {
		input.Status = "pending_activation"
	}
	policy := Policy{
		ID:             newID(),
		TenantID:       input.TenantID,
		Name:           input.Name,
		Composition:    input.Composition,
		ScheduleType:   input.ScheduleType,
		IntervalValue:  input.IntervalValue,
		IntervalUnit:   input.IntervalUnit,
		Hour:           input.Hour,
		Minute:         input.Minute,
		WeekDay:        input.WeekDay,
		MonthDay:       input.MonthDay,
		RetentionCount: input.RetentionCount,
		RetentionDays:  input.RetentionDays,
		Status:         input.Status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[policy.ID] = policy
	return policy, nil
}

func (s *MemoryStore) ListPolicies() ([]Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]Policy, 0, len(s.policies))
	for _, item := range s.policies {
		items = append(items, item)
	}
	return items, nil
}

func (s *MemoryStore) UpdatePolicy(id string, input PolicyInput) (Policy, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.policies[id]
	if !ok {
		return Policy{}, false, nil
	}
	item.Name, item.Composition, item.ScheduleType = input.Name, input.Composition, input.ScheduleType
	item.IntervalValue, item.IntervalUnit, item.Hour, item.Minute = input.IntervalValue, input.IntervalUnit, input.Hour, input.Minute
	item.WeekDay, item.MonthDay, item.RetentionCount, item.RetentionDays = input.WeekDay, input.MonthDay, input.RetentionCount, input.RetentionDays
	item.Status = input.Status
	item.UpdatedAt = time.Now().UTC()
	s.policies[id] = item
	return item, true, nil
}

func (s *MemoryStore) DeletePolicy(id string) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.policies[id]; !ok {
		return false, false, nil
	}
	for _, plan := range s.plans {
		if plan.PolicyID == id {
			return false, true, nil
		}
	}
	delete(s.policies, id)
	return true, false, nil
}

func (s *MemoryStore) CreateProtectionPlan(input ProtectionPlanInput) (ProtectionPlan, error) {
	if input.TenantID == "" {
		input.TenantID = DefaultTenantID
	}
	now := time.Now().UTC()
	if input.ScopeType == "" {
		input.ScopeType = "all"
	}
	if input.Status == "" {
		input.Status = "active"
	}
	if input.ResourceSelection.Mode == "" {
		input.ResourceSelection.Mode = "all"
	}
	appIDs := dedupNonEmpty(append([]string{}, input.AppIDs...))
	if input.AppID != "" {
		appIDs = dedupNonEmpty(append(appIDs, input.AppID))
	}
	primary := ""
	if len(appIDs) > 0 {
		primary = appIDs[0]
	}
	plan := ProtectionPlan{
		ID:                   newID(),
		TenantID:             input.TenantID,
		SourceClusterID:      input.SourceClusterID,
		AppID:                primary,
		AppIDs:               appIDs,
		ScopeType:            input.ScopeType,
		IncludedResources:    input.IncludedResources,
		ResourceSelection:    input.ResourceSelection,
		LabelSelector:        input.LabelSelector,
		IncludeClusterScoped: input.IncludeClusterScoped,
		StorageRepoID:        input.StorageRepoID,
		PolicyID:             input.PolicyID,
		TargetClusterID:      input.TargetClusterID,
		ExcludedResources:    input.ExcludedResources,
		PreHooks:             input.PreHooks,
		PostHooks:            input.PostHooks,
		Status:               input.Status,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.plans {
		if existing.TenantID != plan.TenantID || existing.SourceClusterID != plan.SourceClusterID {
			continue
		}
		existingAppIDs := dedupNonEmpty(append(append([]string{}, existing.AppIDs...), existing.AppID))
		for _, appID := range appIDs {
			for _, existingAppID := range existingAppIDs {
				if appID == existingAppID {
					return ProtectionPlan{}, &ApplicationAlreadyProtectedError{ProtectionPlanID: existing.ID, ApplicationID: appID}
				}
			}
		}
	}
	s.plans[plan.ID] = plan
	return plan, nil
}

func (s *MemoryStore) UpdateProtectionPlanStatus(id string, status string) (ProtectionPlan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan, ok := s.plans[id]
	if !ok {
		return ProtectionPlan{}, false, nil
	}
	if status == "" {
		status = "pending_activation"
	}
	plan.Status = status
	plan.UpdatedAt = time.Now().UTC()
	s.plans[id] = plan
	return plan, true, nil
}

func (s *MemoryStore) UpdateProtectionPlanStorageSize(id string, size map[string]any) (ProtectionPlan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan, ok := s.plans[id]
	if !ok {
		return ProtectionPlan{}, false, nil
	}
	if len(size) == 0 {
		return plan, true, nil
	}
	plan.PlanStorageSize = size
	plan.UpdatedAt = time.Now().UTC()
	s.plans[id] = plan
	return plan, true, nil
}

func (s *MemoryStore) UpsertProtectionPlanSchedule(input ProtectionPlanScheduleInput) (ProtectionPlanSchedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	item, ok := s.schedules[input.ProtectionPlanID]
	if !ok {
		item = ProtectionPlanSchedule{
			ProtectionPlanID: input.ProtectionPlanID,
			CreatedAt:        now,
		}
	}
	item.NextFireAt = input.NextFireAt
	item.Enabled = input.Enabled
	item.UpdatedAt = now
	s.schedules[input.ProtectionPlanID] = item
	return item, nil
}

func (s *MemoryStore) GetProtectionPlanSchedule(planID string) (ProtectionPlanSchedule, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.schedules[planID]
	return item, ok, nil
}

func (s *MemoryStore) ListDueProtectionPlanSchedules(now time.Time) ([]ProtectionPlanSchedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]ProtectionPlanSchedule, 0, len(s.schedules))
	for _, item := range s.schedules {
		if !item.Enabled || item.NextFireAt.IsZero() || item.NextFireAt.After(now) {
			continue
		}
		plan := s.plans[item.ProtectionPlanID]
		if s.tenants[plan.TenantID].Status != "active" {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *MemoryStore) MarkProtectionPlanScheduleFired(input ProtectionPlanScheduleFiredInput) (ProtectionPlanSchedule, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.schedules[input.ProtectionPlanID]
	if !ok {
		return ProtectionPlanSchedule{}, false, nil
	}
	item.LastFiredAt = input.LastFiredAt
	item.NextFireAt = input.NextFireAt
	item.UpdatedAt = time.Now().UTC()
	s.schedules[input.ProtectionPlanID] = item
	return item, true, nil
}

func (s *MemoryStore) DisableProtectionPlanSchedule(planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.schedules[planID]
	if !ok {
		return nil
	}
	item.Enabled = false
	item.UpdatedAt = time.Now().UTC()
	s.schedules[planID] = item
	return nil
}

func (s *MemoryStore) ListProtectionPlans(clusterID string) ([]ProtectionPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]ProtectionPlan, 0, len(s.plans))
	for _, item := range s.plans {
		if clusterID == "" || item.SourceClusterID == clusterID {
			if schedule, ok := s.schedules[item.ID]; ok {
				item.NextFireAt = schedule.NextFireAt
				item.ScheduleEnabled = schedule.Enabled
			}
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *MemoryStore) DeleteProtectionPlan(id string) (ProtectionPlan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan, ok := s.plans[id]
	if !ok {
		return ProtectionPlan{}, false, nil
	}
	delete(s.plans, id)
	appIDs := dedupNonEmpty(append([]string{}, plan.AppIDs...))
	if plan.AppID != "" {
		appIDs = dedupNonEmpty(append(appIDs, plan.AppID))
	}
	plan.AppIDs = appIDs
	for _, appID := range appIDs {
		app, ok := s.applications[appID]
		if !ok {
			continue
		}
		app.ProtectionStatus = "unprotected"
		s.applications[appID] = app
	}
	for taskID, task := range s.tasks {
		if task.ProtectionPlanID != id {
			continue
		}
		if task.Payload == nil {
			task.Payload = map[string]any{}
		}
		task.Payload["archivedProtectionPlanId"] = id
		task.ProtectionPlanID = ""
		s.tasks[taskID] = task
	}
	for pointID, point := range s.restorePoints {
		if point.ProtectionPlanID != id {
			continue
		}
		if point.Metadata == nil {
			point.Metadata = map[string]any{}
		}
		point.Metadata["archivedProtectionPlanId"] = id
		point.ProtectionPlanID = ""
		s.restorePoints[pointID] = point
	}
	return plan, true, nil
}

func (s *MemoryStore) CleanupProtectionPlanRecords(id string) (ProtectionPlan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan, ok := s.plans[id]
	if !ok {
		return ProtectionPlan{}, false, nil
	}
	appIDs := dedupNonEmpty(append([]string{}, plan.AppIDs...))
	if plan.AppID != "" {
		appIDs = dedupNonEmpty(append(appIDs, plan.AppID))
	}
	plan.AppIDs = appIDs
	restorePointIDs := map[string]struct{}{}
	for pointID, point := range s.restorePoints {
		if point.ProtectionPlanID == id {
			restorePointIDs[pointID] = struct{}{}
			delete(s.restorePoints, pointID)
		}
	}
	deletedTaskIDs := map[string]struct{}{}
	for taskID, task := range s.tasks {
		_, restorePointMatch := restorePointIDs[task.RestorePointID]
		if task.ProtectionPlanID == id || restorePointMatch {
			deletedTaskIDs[taskID] = struct{}{}
			delete(s.tasks, taskID)
		}
	}
	if len(deletedTaskIDs) > 0 {
		events := s.taskEvents[:0]
		for _, event := range s.taskEvents {
			if _, deleted := deletedTaskIDs[event.TaskID]; !deleted {
				events = append(events, event)
			}
		}
		s.taskEvents = events
	}
	delete(s.plans, id)
	for _, appID := range appIDs {
		app, ok := s.applications[appID]
		if !ok {
			continue
		}
		stillProtected := false
		for _, other := range s.plans {
			if other.TenantID != plan.TenantID || other.SourceClusterID != plan.SourceClusterID {
				continue
			}
			for _, otherAppID := range dedupNonEmpty(append(append([]string{}, other.AppIDs...), other.AppID)) {
				if otherAppID == appID {
					stillProtected = true
					break
				}
			}
			if stillProtected {
				break
			}
		}
		if !stillProtected {
			app.ProtectionStatus = "pending_protection"
		}
		s.applications[appID] = app
	}
	return plan, true, nil
}

func (s *MemoryStore) CreateRestorePoint(input RestorePointInput) (RestorePoint, error) {
	now := time.Now().UTC()
	pointType := input.PointType
	if pointType == "" {
		pointType = "backup"
	}
	status := input.Status
	if status == "" {
		status = "available"
	}
	tenantID := DefaultTenantID
	if cluster, ok := s.clusters[input.SourceClusterID]; ok {
		tenantID = cluster.TenantID
	}
	point := RestorePoint{
		ID:                newID(),
		TenantID:          tenantID,
		ProtectionPlanID:  input.ProtectionPlanID,
		SourceClusterID:   input.SourceClusterID,
		AppID:             input.AppID,
		StorageRepoID:     input.StorageRepoID,
		DisplayName:       "",
		VeleroBackupName:  input.VeleroBackupName,
		PointType:         pointType,
		Status:            status,
		SizeBytes:         input.SizeBytes,
		StartedAt:         input.StartedAt,
		CompletedAt:       input.CompletedAt,
		ExpiresAt:         input.ExpiresAt,
		SourceNamespace:   input.SourceNamespace,
		LabelSelector:     input.LabelSelector,
		BackupTaskID:      input.BackupTaskID,
		BackupStorageName: input.BackupStorageName,
		TaskCreatedAt:     input.TaskCreatedAt,
		Metadata:          input.Metadata,
		SizeMetricsV2:     input.SizeMetricsV2,
		CreatedAt:         now,
	}
	if point.TaskCreatedAt.IsZero() {
		point.TaskCreatedAt = now
	}
	if point.Metadata == nil {
		point.Metadata = map[string]any{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.restorePoints {
		if existing.SourceClusterID == point.SourceClusterID && existing.VeleroBackupName == point.VeleroBackupName {
			existing.DisplayName = ""
			if existing.SizeBytes == 0 && point.SizeBytes > 0 {
				existing.SizeBytes = point.SizeBytes
			}
			if existing.CompletedAt.IsZero() && !point.CompletedAt.IsZero() {
				existing.CompletedAt = point.CompletedAt
			}
			if existing.Metadata == nil {
				existing.Metadata = map[string]any{}
			}
			for key, value := range point.Metadata {
				existing.Metadata[key] = value
			}
			if len(point.SizeMetricsV2) > 0 {
				existing.SizeMetricsV2 = point.SizeMetricsV2
			}
			s.restorePoints[id] = existing
			return existing, nil
		}
	}
	s.restorePoints[point.ID] = point
	return point, nil
}

func (s *MemoryStore) ListRestorePoints(filter RestorePointFilter) ([]RestorePoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]RestorePoint, 0, len(s.restorePoints))
	for _, item := range s.restorePoints {
		if !filter.IncludeDeleted && item.Status == "deleted" {
			continue
		}
		if !filter.IncludeDeleted {
			plan, ok := s.plans[item.ProtectionPlanID]
			if !ok || plan.SourceClusterID != item.SourceClusterID {
				continue
			}
		}
		if filter.ClusterID != "" && item.SourceClusterID != filter.ClusterID {
			continue
		}
		if filter.AppID != "" && item.AppID != filter.AppID {
			continue
		}
		if filter.ProtectionPlanID != "" && item.ProtectionPlanID != filter.ProtectionPlanID {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *MemoryStore) GetRestorePoint(id string) (RestorePoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.restorePoints[id]
	return item, ok, nil
}

func (s *MemoryStore) UpdateRestorePointState(input RestorePointStateInput) (RestorePoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.restorePoints[input.ID]
	if !ok {
		return RestorePoint{}, false, nil
	}
	if input.Status != "" {
		item.Status = input.Status
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	for key, value := range input.Metadata {
		item.Metadata[key] = value
	}
	s.restorePoints[input.ID] = item
	return item, true, nil
}

func (s *MemoryStore) CreateTask(input TaskInput) (Task, error) {
	now := time.Now().UTC()
	if input.Type == "" {
		input.Type = "backup"
	}
	if input.Status == "" {
		input.Status = "queued"
	}
	tenantID := DefaultTenantID
	if cluster, ok := s.clusters[input.ClusterID]; ok {
		tenantID = cluster.TenantID
	}
	task := Task{
		ID:               newID(),
		TenantID:         tenantID,
		ClusterID:        input.ClusterID,
		AppID:            input.AppID,
		ProtectionPlanID: input.ProtectionPlanID,
		RestorePointID:   input.RestorePointID,
		Type:             input.Type,
		Status:           input.Status,
		CommandID:        input.CommandID,
		Payload:          input.Payload,
		CreatedAt:        now,
	}
	if task.Payload == nil {
		task.Payload = map[string]any{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
	return task, nil
}

func (s *MemoryStore) ListTasks(clusterID string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]Task, 0, len(s.tasks))
	for _, item := range s.tasks {
		if clusterID == "" || item.ClusterID == clusterID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *MemoryStore) UpdateTaskStatus(input TaskStatusInput) (Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[input.TaskID]
	if !ok {
		return Task{}, false, nil
	}
	now := time.Now().UTC()
	if input.Status != "" {
		if task.CompletedAt.IsZero() || !isActiveStatus(input.Status) {
			task.Status = input.Status
		}
	}
	if input.Progress > task.Progress {
		task.Progress = input.Progress
	}
	if input.RestorePointID != "" {
		task.RestorePointID = input.RestorePointID
	}
	task.ErrorCode = input.ErrorCode
	task.ErrorMessage = input.ErrorMessage
	if input.MarkAccepted {
		task.AcceptedAt = now
	}
	if input.MarkStarted && task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	if input.MarkDone {
		task.CompletedAt = now
	}
	if task.Status == "dispatched" && task.DispatchedAt.IsZero() {
		task.DispatchedAt = now
	}
	if len(input.Payload) > 0 {
		if task.Payload == nil {
			task.Payload = map[string]any{}
		}
		for key, value := range input.Payload {
			task.Payload[key] = value
		}
	}
	s.tasks[task.ID] = task
	return task, true, nil
}

func isActiveStatus(status string) bool {
	switch status {
	case "queued", "dispatched", "accepted", "running", "syncing", "finalizing", "canceling":
		return true
	default:
		return false
	}
}

func (s *MemoryStore) AddTaskEvent(input TaskEventInput) error {
	event := TaskEvent{
		ID:        newID(),
		TaskID:    input.TaskID,
		Level:     input.Level,
		Reason:    input.Reason,
		Message:   input.Message,
		Payload:   input.Payload,
		CreatedAt: time.Now().UTC(),
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskEvents = append(s.taskEvents, event)
	if task, ok := s.tasks[input.TaskID]; ok {
		s.diagnosticLogs = append(s.diagnosticLogs, DiagnosticLog{ID: newID(), TenantID: task.TenantID, Scope: "tenant", Level: normalizeDiagnosticLevel(input.Level), Component: "task", Operation: task.Type, Message: input.Message, ClusterID: task.ClusterID, TaskID: task.ID, CommandID: task.CommandID, ErrorCode: input.Reason, Status: task.Status, Details: redactDiagnosticDetails(input.Payload), EventAt: event.CreatedAt, CreatedAt: event.CreatedAt})
	}
	return nil
}

func (s *MemoryStore) CreateDiagnosticLog(input DiagnosticLogInput) (DiagnosticLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := diagnosticLogFromInput(input, time.Now().UTC())
	if item.Fingerprint != "" {
		for _, existing := range s.diagnosticLogs {
			if existing.Fingerprint == item.Fingerprint {
				return existing, nil
			}
		}
	}
	s.diagnosticLogs = append(s.diagnosticLogs, item)
	return item, nil
}

func (s *MemoryStore) ListDiagnosticLogs(filter DiagnosticLogFilter) ([]DiagnosticLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]DiagnosticLog, 0)
	for i := len(s.diagnosticLogs) - 1; i >= 0; i-- {
		item := s.diagnosticLogs[i]
		if diagnosticLogMatches(item, filter) {
			items = append(items, item)
		}
	}
	return paginateDiagnosticLogs(items, filter), nil
}

func (s *MemoryStore) PurgeDiagnosticLogs(before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]DiagnosticLog, 0, len(s.diagnosticLogs))
	var removed int64
	for _, item := range s.diagnosticLogs {
		if item.EventAt.Before(before) {
			removed++
			continue
		}
		kept = append(kept, item)
	}
	s.diagnosticLogs = kept
	for key, coverage := range s.logCoverage {
		if coverage.CoveredTo.Before(before) {
			delete(s.logCoverage, key)
			continue
		}
		if coverage.CoveredFrom.Before(before) {
			coverage.CoveredFrom = before.UTC()
			coverage.UpdatedAt = time.Now().UTC()
			s.logCoverage[key] = coverage
		}
	}
	return removed, nil
}

func logCoverageKey(clusterID, component string) string { return clusterID + "::" + component }

func (s *MemoryStore) GetClusterLogCoverage(clusterID string, component string) (ClusterLogCoverage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.logCoverage[logCoverageKey(clusterID, component)]
	return item, ok, nil
}

func (s *MemoryStore) UpsertClusterLogCoverage(input ClusterLogCoverageInput) (ClusterLogCoverage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := logCoverageKey(input.ClusterID, input.Component)
	now := time.Now().UTC()
	item := ClusterLogCoverage{ClusterID: input.ClusterID, TenantID: input.TenantID, Component: input.Component, CoveredFrom: input.CoveredFrom.UTC(), CoveredTo: input.CoveredTo.UTC(), LastCollectedAt: input.CollectedAt.UTC(), LastRequestID: input.RequestID, LastEntryCount: input.EntryCount, Truncated: input.Truncated, UpdatedAt: now}
	if existing, ok := s.logCoverage[key]; ok {
		// Merge only overlapping/adjacent ranges. A long offline gap must not be
		// represented as continuously covered.
		if !item.CoveredFrom.After(existing.CoveredTo.Add(5 * time.Minute)) {
			if existing.CoveredFrom.Before(item.CoveredFrom) {
				item.CoveredFrom = existing.CoveredFrom
			}
			if existing.CoveredTo.After(item.CoveredTo) {
				item.CoveredTo = existing.CoveredTo
			}
			item.Truncated = existing.Truncated || item.Truncated
		}
	}
	s.logCoverage[key] = item
	return item, nil
}

func (s *MemoryStore) ListTaskEvents(taskID string) ([]TaskEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]TaskEvent, 0)
	for _, item := range s.taskEvents {
		if item.TaskID == taskID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *MemoryStore) GetProtectionPlan(id string) (ProtectionPlan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	if !ok {
		return ProtectionPlan{}, false, nil
	}
	if len(p.AppIDs) == 0 && p.AppID != "" {
		p.AppIDs = []string{p.AppID}
	}
	if schedule, ok := s.schedules[p.ID]; ok {
		p.NextFireAt = schedule.NextFireAt
		p.ScheduleEnabled = schedule.Enabled
	}
	return p, true, nil
}

func (s *MemoryStore) GetApplication(id string) (Application, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.applications {
		if a.ID == id {
			return a, true, nil
		}
	}
	return Application{}, false, nil
}
