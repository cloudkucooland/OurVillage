package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentmud/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	return database
}

func TestRegisterCharacterSuccess(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := DefaultConfig()
	cfg.Server.HTTPAddr = "" // disable http listener in test
	srv := &Server{
		db:     database,
		config: cfg,
	}

	req := RegisterRequest{
		ID:             "new_adventurer",
		Name:           "New Adventurer",
		Occupation:     "Adventurer",
		PersonaPrompt:  "An eager newcomer to OurVillage.",
		LocationID:     "market",
		ControllerType: "human",
	}

	password, err := srv.RegisterCharacter(req)
	if err != nil {
		t.Fatalf("RegisterCharacter() error = %v", err)
	}
	if password == "" {
		t.Fatalf("expected non-empty mqtt password")
	}

	// Verify character in database
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM characters WHERE id = ?", "new_adventurer").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("failed to find registered character in database: %v", err)
	}
}

func TestRegisterCharacterValidationAndConflicts(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := DefaultConfig()
	cfg.Server.HTTPAddr = ""
	srv := &Server{
		db:     database,
		config: cfg,
	}

	// 1. Invalid ID
	_, err := srv.RegisterCharacter(RegisterRequest{
		ID:            "invalid id!",
		Name:          "Bad",
		Occupation:    "Thief",
		PersonaPrompt: "Bad id",
	})
	if err == nil {
		t.Fatalf("expected error for invalid character ID, got nil")
	}

	// 2. Duplicate ID (eldon_millwright is seeded)
	_, err = srv.RegisterCharacter(RegisterRequest{
		ID:            "eldon_millwright",
		Name:          "Eldon Fake",
		Occupation:    "Millwright",
		PersonaPrompt: "Fake eldon",
	})
	if err == nil {
		t.Fatalf("expected error for duplicate character ID, got nil")
	}
}

func TestRegisterCharacterInviteTokens(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := DefaultConfig()
	cfg.Server.HTTPAddr = ""
	cfg.Server.InviteTokens = []string{"secret-invite-123"}
	srv := &Server{
		db:     database,
		config: cfg,
	}

	// 1. Missing / wrong invite token
	_, err := srv.RegisterCharacter(RegisterRequest{
		ID:            "token_test",
		Name:          "Token Test",
		Occupation:    "Tester",
		PersonaPrompt: "Testing tokens",
		InviteToken:   "wrong-token",
	})
	if err == nil {
		t.Fatalf("expected error for incorrect invite token, got nil")
	}

	// 2. Correct invite token
	_, err = srv.RegisterCharacter(RegisterRequest{
		ID:            "token_test",
		Name:          "Token Test",
		Occupation:    "Tester",
		PersonaPrompt: "Testing tokens",
		InviteToken:   "secret-invite-123",
	})
	if err != nil {
		t.Fatalf("RegisterCharacter() with valid token error = %v", err)
	}
}

func TestRegisterAPIEndpoint(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := DefaultConfig()
	srv := &Server{
		db:     database,
		config: cfg,
	}

	regReq := RegisterRequest{
		ID:            "api_user",
		Name:          "API User",
		Occupation:    "Merchant",
		PersonaPrompt: "API registered user",
	}
	body, _ := json.Marshal(regReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/characters/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.handleRegisterAPI(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp RegisterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if !resp.Success || resp.MQTTPassword == "" {
		t.Fatalf("unexpected API response: %+v", resp)
	}
}
