package scim

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra1/model-gateway/internal/auth/rbac"
)

const (
	SchemaUser         = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroup        = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaListResponse = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaError        = "urn:ietf:params:scim:api:messages:2.0:Error"
)

// KeyRevoker defines the interface to deactivate virtual keys when a user is de-provisioned.
type KeyRevoker interface {
	RevokeKeysByUserID(userID string) (int, error)
}

// Name represents RFC 7643 User Name structure.
type Name struct {
	Formatted  string `json:"formatted,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
}

// Email represents an email address in SCIM.
type Email struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// UserRole represents a role assignment in SCIM.
type UserRole struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

// GroupRef represents group membership on a user object.
type GroupRef struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

// Meta describes resource metadata.
type Meta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location,omitempty"`
	Version      string    `json:"version,omitempty"`
}

// User represents a SCIM 2.0 User resource.
type User struct {
	Schemas     []string   `json:"schemas"`
	ID          string     `json:"id"`
	ExternalID  string     `json:"externalId,omitempty"`
	UserName    string     `json:"userName"`
	Name        Name       `json:"name,omitempty"`
	DisplayName string     `json:"displayName,omitempty"`
	Emails      []Email    `json:"emails,omitempty"`
	Roles       []UserRole `json:"roles,omitempty"`
	Groups      []GroupRef `json:"groups,omitempty"`
	Active      bool       `json:"active"`
	Meta        Meta       `json:"meta"`
}

// GroupMember represents a member in a SCIM Group resource.
type GroupMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

// Group represents a SCIM 2.0 Group resource.
type Group struct {
	Schemas     []string      `json:"schemas"`
	ID          string        `json:"id"`
	DisplayName string        `json:"displayName"`
	Members     []GroupMember `json:"members,omitempty"`
	Meta        Meta          `json:"meta"`
}

// ListResponse represents a paginated SCIM response.
type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []any    `json:"Resources"`
}

// ErrorResponse represents a standard RFC 7644 Error message.
type ErrorResponse struct {
	Schemas  []string `json:"schemas"`
	Detail   string   `json:"detail"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
}

// Service manages SCIM 2.0 user and group synchronization.
type Service struct {
	mu         sync.RWMutex
	authToken  string
	keyRevoker KeyRevoker
	users      map[string]*User
	usersByName map[string]*User
	groups     map[string]*Group
	baseURL    string
}

// NewService creates a new SCIM 2.0 service.
func NewService(authToken string, revoker KeyRevoker) *Service {
	return &Service{
		authToken:   authToken,
		keyRevoker:  revoker,
		users:       make(map[string]*User),
		usersByName: make(map[string]*User),
		groups:      make(map[string]*Group),
		baseURL:     "/scim/v2",
	}
}

func genID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeSCIMError(w http.ResponseWriter, status int, detail string, scimType string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Schemas:  []string{SchemaError},
		Detail:   detail,
		Status:   strconv.Itoa(status),
		ScimType: scimType,
	})
}

// ServeHTTP handles SCIM 2.0 REST routing.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Bearer Token Authentication
	authHeader := r.Header.Get("Authorization")
	if s.authToken != "" {
		expected := "Bearer " + s.authToken
		if authHeader != expected {
			writeSCIMError(w, http.StatusUnauthorized, "Invalid or missing Bearer authorization token", "")
			return
		}
	}

	path := strings.TrimPrefix(r.URL.Path, s.baseURL)
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeSCIMError(w, http.StatusNotFound, "SCIM endpoint not found", "")
		return
	}

	switch strings.ToLower(parts[0]) {
	case "users":
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				s.handleListUsers(w, r)
			case http.MethodPost:
				s.handleCreateUser(w, r)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		} else if len(parts) == 2 {
			userID := parts[1]
			switch r.Method {
			case http.MethodGet:
				s.handleGetUser(w, r, userID)
			case http.MethodPut, http.MethodPatch:
				s.handleUpdateUser(w, r, userID)
			case http.MethodDelete:
				s.handleDeleteUser(w, r, userID)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		}
	case "groups":
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				s.handleListGroups(w, r)
			case http.MethodPost:
				s.handleCreateGroup(w, r)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		} else if len(parts) == 2 {
			groupID := parts[1]
			switch r.Method {
			case http.MethodGet:
				s.handleGetGroup(w, r, groupID)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		}
	}

	writeSCIMError(w, http.StatusNotFound, "resource not found", "")
}

func (s *Service) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	type userInput struct {
		User
		Active *bool `json:"active,omitempty"`
	}
	var in userInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error(), "invalidSyntax")
		return
	}
	input := in.User

	if input.UserName == "" {
		writeSCIMError(w, http.StatusBadRequest, "userName is required", "invalidValue")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.usersByName[input.UserName]; exists {
		writeSCIMError(w, http.StatusConflict, "User with userName already exists", "uniqueness")
		return
	}

	id := "usr-" + genID()
	now := time.Now().UTC()

	// Default role is Engineer if not specified
	if len(input.Roles) == 0 {
		input.Roles = []UserRole{{Value: string(rbac.RoleEngineer), Primary: true}}
	}

	active := true
	if in.Active != nil {
		active = *in.Active
	}

	user := &User{
		Schemas:     []string{SchemaUser},
		ID:          id,
		ExternalID:  input.ExternalID,
		UserName:    input.UserName,
		Name:        input.Name,
		DisplayName: input.DisplayName,
		Emails:      input.Emails,
		Roles:       input.Roles,
		Groups:      input.Groups,
		Active:      active,
		Meta: Meta{
			ResourceType: "User",
			Created:      now,
			LastModified: now,
			Location:     fmt.Sprintf("%s/Users/%s", s.baseURL, id),
			Version:      "W/\"1\"",
		},
	}

	s.users[id] = user
	s.usersByName[user.UserName] = user

	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(user)
}

func (s *Service) handleGetUser(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.RLock()
	user, exists := s.users[id]
	s.mu.RUnlock()

	if !exists {
		writeSCIMError(w, http.StatusNotFound, "User not found", "")
		return
	}

	w.Header().Set("Content-Type", "application/scim+json")
	_ = json.NewEncoder(w).Encode(user)
}

func (s *Service) handleListUsers(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]any, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}

	resp := ListResponse{
		Schemas:      []string{SchemaListResponse},
		TotalResults: len(users),
		StartIndex:   1,
		ItemsPerPage: len(users),
		Resources:    users,
	}

	w.Header().Set("Content-Type", "application/scim+json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Service) handleUpdateUser(w http.ResponseWriter, r *http.Request, id string) {
	var input map[string]any
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error(), "invalidSyntax")
		return
	}

	s.mu.Lock()
	user, exists := s.users[id]
	if !exists {
		s.mu.Unlock()
		writeSCIMError(w, http.StatusNotFound, "User not found", "")
		return
	}

	var deactivated bool

	// Check if active status changed
	if val, ok := input["active"]; ok {
		if boolVal, isBool := val.(bool); isBool {
			if !boolVal {
				deactivated = true
			}
			user.Active = boolVal
		}
	}

	// Update DisplayName if provided
	if val, ok := input["displayName"].(string); ok && val != "" {
		user.DisplayName = val
	}

	user.Meta.LastModified = time.Now().UTC()
	s.mu.Unlock()

	// If user was deactivated, revoke keys
	if deactivated && s.keyRevoker != nil {
		_, _ = s.keyRevoker.RevokeKeysByUserID(id)
	}

	w.Header().Set("Content-Type", "application/scim+json")
	_ = json.NewEncoder(w).Encode(user)
}

func (s *Service) handleDeleteUser(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	user, exists := s.users[id]
	if !exists {
		s.mu.Unlock()
		writeSCIMError(w, http.StatusNotFound, "User not found", "")
		return
	}

	delete(s.users, id)
	delete(s.usersByName, user.UserName)
	s.mu.Unlock()

	if s.keyRevoker != nil {
		_, _ = s.keyRevoker.RevokeKeysByUserID(id)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var input Group
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error(), "invalidSyntax")
		return
	}

	if input.DisplayName == "" {
		writeSCIMError(w, http.StatusBadRequest, "displayName is required", "invalidValue")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id := "grp-" + genID()
	now := time.Now().UTC()

	group := &Group{
		Schemas:     []string{SchemaGroup},
		ID:          id,
		DisplayName: input.DisplayName,
		Members:     input.Members,
		Meta: Meta{
			ResourceType: "Group",
			Created:      now,
			LastModified: now,
			Location:     fmt.Sprintf("%s/Groups/%s", s.baseURL, id),
		},
	}

	s.groups[id] = group

	// Update members' user records with group reference
	for _, m := range group.Members {
		if u, ok := s.users[m.Value]; ok {
			u.Groups = append(u.Groups, GroupRef{
				Value:   group.ID,
				Display: group.DisplayName,
				Ref:     group.Meta.Location,
			})
		}
	}

	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(group)
}

func (s *Service) handleGetGroup(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.RLock()
	group, exists := s.groups[id]
	s.mu.RUnlock()

	if !exists {
		writeSCIMError(w, http.StatusNotFound, "Group not found", "")
		return
	}

	w.Header().Set("Content-Type", "application/scim+json")
	_ = json.NewEncoder(w).Encode(group)
}

func (s *Service) handleListGroups(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	groups := make([]any, 0, len(s.groups))
	for _, g := range s.groups {
		groups = append(groups, g)
	}

	resp := ListResponse{
		Schemas:      []string{SchemaListResponse},
		TotalResults: len(groups),
		StartIndex:   1,
		ItemsPerPage: len(groups),
		Resources:    groups,
	}

	w.Header().Set("Content-Type", "application/scim+json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetUserByID retrieves a user by ID directly from memory.
func (s *Service) GetUserByID(id string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok
}
