package store

import (
	"errors"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu            sync.Mutex
	tokens        map[string]AgentToken
	credentials   map[string]string
	clusters      map[string]Cluster
	applications  map[string]Application
	storage       map[string]StorageRepository
	bindings      map[string]ClusterStorageBinding
	policies      map[string]Policy
	plans         map[string]ProtectionPlan
	schedules     map[string]ProtectionPlanSchedule
	restorePoints map[string]RestorePoint
	tasks         map[string]Task
	taskEvents    []TaskEvent
	users         map[string]memoryUser
	resetTokens   map[string]memoryResetToken
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
		tokens:        map[string]AgentToken{},
		credentials:   map[string]string{},
		clusters:      map[string]Cluster{},
		applications:  map[string]Application{},
		storage:       map[string]StorageRepository{},
		bindings:      map[string]ClusterStorageBinding{},
		policies:      map[string]Policy{},
		plans:         map[string]ProtectionPlan{},
		schedules:     map[string]ProtectionPlanSchedule{},
		restorePoints: map[string]RestorePoint{},
		tasks:         map[string]Task{},
		taskEvents:    []TaskEvent{},
		users:         map[string]memoryUser{DefaultAdminEmail: {User: User{ID: "00000000-0000-0000-0000-00000000a001", TenantID: DefaultTenantID, Email: DefaultAdminEmail, Role: "admin", Status: "active"}, Password: DefaultAdminPassword}},
		resetTokens:   map[string]memoryResetToken{},
	}
}

func (s *MemoryStore) AuthenticateUser(input UserAuthInput) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[strings.ToLower(strings.TrimSpace(input.Email))]
	if !ok || u.Password != input.Password {
		return User{}, false, nil
	}
	return u.User, true, nil
}

func (s *MemoryStore) CreateUser(email, password string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	if _, ok := s.users[email]; ok {
		return User{}, ErrUserExists
	}
	u := User{ID: newID(), TenantID: DefaultTenantID, Email: email, Role: "member", Status: "active"}
	s.users[email] = memoryUser{User: u, Password: password}
	return u, nil
}

func (s *MemoryStore) CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	if _, ok := s.users[email]; !ok {
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
	u := User{ID: newID(), TenantID: DefaultTenantID, Email: email, Role: "member", Status: "active"}
	s.users[email] = memoryUser{User: u}
	return u, nil
}

func (s *MemoryStore) CreateAgentToken(description string, ttl time.Duration) (AgentToken, error) {
	now := time.Now().UTC()
	token := AgentToken{
		ID:          newID(),
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

	cluster := Cluster{
		ID:               newID(),
		TenantID:         DefaultTenantID,
		Name:             clusterName,
		KubeVersion:      input.KubeVersion,
		Status:           "healthy",
		ConnectionStatus: "online",
		AgentVersion:     input.AgentVersion,
		VeleroVersion:    input.VeleroVersion,
		VeleroStatus:     input.VeleroStatus,
		Role:             "both",
		IsDefault:        len(s.clusters) == 0,
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
				item.IsDefault = false
				s.clusters[id] = item
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
		TenantID:   DefaultTenantID,
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
	sourceClusterID := input.SourceClusterID
	if sourceClusterID == "" {
		sourceClusterID = input.ClusterID
	}
	key := input.ClusterID + ":" + input.StorageRepoID + ":" + sourceClusterID
	item, ok := s.bindings[key]
	if !ok {
		item = ClusterStorageBinding{
			ID:              newID(),
			TenantID:        DefaultTenantID,
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
		TenantID:       DefaultTenantID,
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

func (s *MemoryStore) CreateProtectionPlan(input ProtectionPlanInput) (ProtectionPlan, error) {
	now := time.Now().UTC()
	if input.ScopeType == "" {
		input.ScopeType = "all"
	}
	if input.Status == "" {
		input.Status = "active"
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
		TenantID:             DefaultTenantID,
		SourceClusterID:      input.SourceClusterID,
		AppID:                primary,
		AppIDs:               appIDs,
		ScopeType:            input.ScopeType,
		IncludedResources:    input.IncludedResources,
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
		app.ProtectionStatus = "pending_protection"
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
		app.ProtectionStatus = "pending_protection"
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
	point := RestorePoint{
		ID:                newID(),
		TenantID:          DefaultTenantID,
		ProtectionPlanID:  input.ProtectionPlanID,
		SourceClusterID:   input.SourceClusterID,
		AppID:             input.AppID,
		StorageRepoID:     input.StorageRepoID,
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
		Metadata:          input.Metadata,
		CreatedAt:         now,
	}
	if point.Metadata == nil {
		point.Metadata = map[string]any{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.restorePoints {
		if existing.SourceClusterID == point.SourceClusterID && existing.VeleroBackupName == point.VeleroBackupName {
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
	task := Task{
		ID:               newID(),
		TenantID:         DefaultTenantID,
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
	return nil
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
