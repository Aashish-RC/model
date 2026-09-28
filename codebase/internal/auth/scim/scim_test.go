package scim

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ra1/model-gateway/internal/auth/rbac"
	"github.com/ra1/model-gateway/internal/store"
)

func TestSCIM_CreateUser_AssignsRoleAndGeneratesID(t *testing.T) {
	keyStore := store.NewMemoryKeyStore()
	svc := NewService("secret-scim-token", keyStore)

	payload := `{
		"userName": "alice@corp.example.com",
		"name": {
			"formatted": "Alice Wonderland",
			"familyName": "Wonderland",
			"givenName": "Alice"
		},
		"emails": [{"value": "alice@corp.example.com", "primary": true}],
		"roles": [{"value": "admin", "primary": true}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer secret-scim-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	svc.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var created User
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if created.ID == "" {
		t.Errorf("expected generated ID, got empty")
	}
	if created.UserName != "alice@corp.example.com" {
		t.Errorf("expected alice@corp.example.com, got %s", created.UserName)
	}
	if len(created.Roles) == 0 || created.Roles[0].Value != string(rbac.RoleAdmin) {
		t.Errorf("expected role admin, got %+v", created.Roles)
	}
	if !created.Active {
		t.Errorf("expected user to be active")
	}
}

func TestSCIM_DeactivateUser_RevokesActiveVirtualKeys(t *testing.T) {
	keyStore := store.NewMemoryKeyStore()
	svc := NewService("secret-scim-token", keyStore)

	// 1. Create User via SCIM
	userPayload := `{
		"userName": "bob@corp.example.com",
		"emails": [{"value": "bob@corp.example.com", "primary": true}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(userPayload))
	req.Header.Set("Authorization", "Bearer secret-scim-token")
	w := httptest.NewRecorder()
	svc.ServeHTTP(w, req)

	var user User
	_ = json.Unmarshal(w.Body.Bytes(), &user)
	userID := user.ID

	// 2. Issue a virtual key bound to this user
	rawKey, keyHash, prefix, _ := store.GenerateRawKey("live")
	keyObj := &store.VirtualKey{
		ID:        "vk-bob-1",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		UserID:    userID,
		IsActive:  true,
	}
	if err := keyStore.SaveKey(keyObj); err != nil {
		t.Fatalf("failed to save key: %v", err)
	}

	// Verify key authenticates before deactivation
	if _, err := keyStore.Authenticate(rawKey, "gpt-4o"); err != nil {
		t.Fatalf("expected key to be active, got error: %v", err)
	}

	// 3. Deactivate User via SCIM PATCH/PUT (active: false)
	deactPayload := `{"active": false}`
	patchReq := httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/"+userID, bytes.NewBufferString(deactPayload))
	patchReq.Header.Set("Authorization", "Bearer secret-scim-token")
	wPatch := httptest.NewRecorder()
	svc.ServeHTTP(wPatch, patchReq)

	if wPatch.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from patch, got %d: %s", wPatch.Code, wPatch.Body.String())
	}

	// 4. Verify Virtual Key is now deactivated
	_, err := keyStore.Authenticate(rawKey, "gpt-4o")
	if err != store.ErrKeyInactive {
		t.Errorf("expected ErrKeyInactive after SCIM deactivation, got %v", err)
	}
}

func TestSCIM_GroupSync_MapsToGatewayTeams(t *testing.T) {
	keyStore := store.NewMemoryKeyStore()
	svc := NewService("secret-scim-token", keyStore)

	// Create user
	uReq := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(`{"userName": "charlie@corp.example.com"}`))
	uReq.Header.Set("Authorization", "Bearer secret-scim-token")
	wU := httptest.NewRecorder()
	svc.ServeHTTP(wU, uReq)
	var u User
	_ = json.Unmarshal(wU.Body.Bytes(), &u)

	// Create Group with member Charlie
	groupPayload := `{
		"displayName": "engineering-ml",
		"members": [
			{"value": "` + u.ID + `", "display": "charlie@corp.example.com"}
		]
	}`
	gReq := httptest.NewRequest(http.MethodPost, "/scim/v2/Groups", bytes.NewBufferString(groupPayload))
	gReq.Header.Set("Authorization", "Bearer secret-scim-token")
	wG := httptest.NewRecorder()
	svc.ServeHTTP(wG, gReq)

	if wG.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for Group, got %d: %s", wG.Code, wG.Body.String())
	}

	// Verify User now has group membership
	updatedUser, ok := svc.GetUserByID(u.ID)
	if !ok {
		t.Fatalf("user not found")
	}
	if len(updatedUser.Groups) == 0 {
		t.Fatalf("expected user to be in group")
	}
	if updatedUser.Groups[0].Display != "engineering-ml" {
		t.Errorf("expected group display 'engineering-ml', got %s", updatedUser.Groups[0].Display)
	}
}

func TestSCIM_UnauthorizedRequest_Rejects401(t *testing.T) {
	svc := NewService("secret-scim-token", nil)

	// Missing Authorization header
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	w := httptest.NewRecorder()
	svc.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized for missing token, got %d", w.Code)
	}

	// Invalid Bearer token
	req = httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	svc.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized for invalid token, got %d", w.Code)
	}
}
