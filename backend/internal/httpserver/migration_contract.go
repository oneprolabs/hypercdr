package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type migrationAuthorizationResponse struct {
	ID              string    `json:"id"`
	Token           string    `json:"token"`
	ExpiresAt       time.Time `json:"expiresAt"`
	ProtocolVersion string    `json:"protocolVersion"`
}
type migrationSessionRequest struct {
	Token            string `json:"token"`
	TargetInstanceID string `json:"targetInstanceId"`
	ProtocolVersion  string `json:"protocolVersion"`
	TargetPublicKey  string `json:"targetPublicKey"`
}
type migrationSessionResponse struct {
	ID               string    `json:"id"`
	SessionToken     string    `json:"sessionToken"`
	SourceInstanceID string    `json:"sourceInstanceId"`
	TargetInstanceID string    `json:"targetInstanceId"`
	ProtocolVersion  string    `json:"protocolVersion"`
	State            string    `json:"state"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

type migrationInventoryResponse struct {
	MigrationID         string              `json:"migrationId"`
	SourceInstanceID    string              `json:"sourceInstanceId"`
	ProtocolVersion     string              `json:"protocolVersion"`
	ExportFormatVersion string              `json:"exportFormatVersion"`
	SchemaVersions      []string            `json:"schemaVersions"`
	ManifestAvailable   bool                `json:"manifestAvailable"`
	AgentNamespace      string              `json:"agentNamespace"`
	Counts              map[string]int      `json:"counts"`
	ActiveTasks         []map[string]string `json:"activeTasks"`
	Ready               bool                `json:"ready"`
}
type migrationBackupResponse struct {
	Migration store.CommunityMigrationSession        `json:"migration"`
	Manifest  store.CommunityMigrationExportManifest `json:"manifest"`
}
type migrationCredentialsResponse struct {
	Algorithm string                        `json:"algorithm"`
	Items     []migrationCredentialEnvelope `json:"items"`
}
type migrationSMTPItem struct {
	ID               string                       `json:"id"`
	Name             string                       `json:"name"`
	Enabled          bool                         `json:"enabled"`
	Host             string                       `json:"host"`
	Port             int                          `json:"port"`
	Security         string                       `json:"security"`
	Username         string                       `json:"username"`
	SenderName       string                       `json:"senderName"`
	SenderEmail      string                       `json:"senderEmail"`
	PasswordEnvelope *migrationCredentialEnvelope `json:"passwordEnvelope,omitempty"`
}
type migrationSMTPResponse struct {
	Items            []migrationSMTPItem `json:"items"`
	DefaultPreserved bool                `json:"defaultPreserved"`
	ImportMode       string              `json:"importMode"`
}
type migrationHandoverRequest struct {
	TargetEndpoint   string            `json:"targetEndpoint"`
	RollbackDeadline time.Time         `json:"rollbackDeadline"`
	ClusterTokens    map[string]string `json:"clusterTokens"`
}
type migrationHandoverResponse struct {
	Migration        store.CommunityMigrationSession `json:"migration"`
	Tasks            []store.Task                    `json:"tasks"`
	RollbackDeadline time.Time                       `json:"rollbackDeadline"`
}
type migrationCommitResponse struct {
	Migration         store.CommunityMigrationSession `json:"migration"`
	ObservationEndsAt time.Time                       `json:"observationEndsAt"`
	ReadOnly          bool                            `json:"readOnly"`
	AutomaticDeletion bool                            `json:"automaticDeletion"`
}

func applyMigrationPayloadContract(pattern string, operation map[string]any) bool {
	c := payloadContract{}
	status := 200
	switch pattern {
	case "GET /api/v1/community-migrations":
		c.Response = reflect.TypeFor[listResponse[store.CommunityMigrationSession]]()
	case "POST /api/v1/community-migrations/authorizations":
		c.Response = reflect.TypeFor[migrationAuthorizationResponse]()
		status = 201
	case "POST /api/v1/community-migrations/source/sessions":
		c.Request = reflect.TypeFor[migrationSessionRequest]()
		c.Response = reflect.TypeFor[migrationSessionResponse]()
		c.Required = []string{"token", "targetInstanceId", "protocolVersion", "targetPublicKey"}
		status = 201
	case "GET /api/v1/community-migrations/source/{id}/inventory":
		c.Response = reflect.TypeFor[migrationInventoryResponse]()
	case "POST /api/v1/community-migrations/source/{id}/backup":
		c.Response = reflect.TypeFor[migrationBackupResponse]()
	case "GET /api/v1/community-migrations/source/{id}/manifest":
		c.Response = reflect.TypeFor[store.CommunityMigrationExportManifest]()
	case "GET /api/v1/community-migrations/source/{id}/export/{table}":
		c.Response = reflect.TypeFor[store.CommunityMigrationExportBatch]()
	case "GET /api/v1/community-migrations/source/{id}/credentials":
		c.Response = reflect.TypeFor[migrationCredentialsResponse]()
	case "GET /api/v1/community-migrations/source/{id}/smtp":
		c.Response = reflect.TypeFor[migrationSMTPResponse]()
	case "POST /api/v1/community-migrations/source/{id}/freeze", "POST /api/v1/community-migrations/source/{id}/rollback":
		c.Response = reflect.TypeFor[store.CommunityMigrationSession]()
	case "POST /api/v1/community-migrations/source/{id}/handover":
		c.Request = reflect.TypeFor[migrationHandoverRequest]()
		c.Response = reflect.TypeFor[migrationHandoverResponse]()
		c.Required = []string{"targetEndpoint", "rollbackDeadline", "clusterTokens"}
		status = 202
	case "POST /api/v1/community-migrations/source/{id}/commit":
		c.Response = reflect.TypeFor[migrationCommitResponse]()
	default:
		return false
	}
	applyPayloadShape(operation, c, status)
	operation["description"] = "Installation-wide migration. History and authorization creation require System Administrator. Session opening consumes a one-use authorization body token (30-minute expiry), establishes a two-hour session, and requires the v1 migration protocol."
	if pattern == "POST /api/v1/community-migrations/source/sessions" {
		operation["x-hypercdr-body-token-auth"] = "token"
		schema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		properties["token"].(map[string]any)["writeOnly"] = true
		properties["protocolVersion"].(map[string]any)["enum"] = []string{communityMigrationProtocolV1}
	}
	if strings.Contains(pattern, "/source/{id}/") {
		operation["description"] = "Requires Authorization: Migration <sessionToken>, bound to path ID. Platform Bearer/release tokens cannot substitute. Source data and state changes are installation-wide; encrypted credentials target the session public key. Backup/export/handover state gates are enforced."
	}
	if strings.Contains(pattern, "/export/") {
		schema := operation["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		schema["properties"].(map[string]any)["rows"] = map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "Frozen table records; table-specific columns and hashes follow the versioned migration export protocol."}
		for _, name := range []string{"limit", "offset"} {
			operation["parameters"] = append(operation["parameters"].([]any), map[string]any{"name": name, "in": "query", "schema": map[string]any{"type": "integer"}})
		}
	}
	responses := operation["responses"].(map[string]any)
	for _, code := range []int{400, 401, 403, 409, 410, 500, 501, 502} {
		responses[strconv.Itoa(code)] = map[string]any{"description": "Invalid input/token/path binding, denied administrator, state or task conflict, expired token, persistence/export/agent failure", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiError"}}}}
	}
	return true
}
