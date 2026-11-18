package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"agentmud/internal/db"
)

type RegisterRequest struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Occupation     string `json:"occupation"`
	PersonaPrompt  string `json:"persona_prompt"`
	LocationID     string `json:"location_id"`
	ControllerType string `json:"controller_type"` // "human" or "agent"
	InviteToken    string `json:"invite_token"`
}

type RegisterResponse struct {
	Success      bool   `json:"success"`
	CharacterID  string `json:"character_id"`
	MQTTPassword string `json:"mqtt_password"`
	Message      string `json:"message,omitempty"`
}

func (s *Server) RegisterCharacter(req RegisterRequest) (string, error) {
	if !s.config.Server.EnableSelfRegistration {
		return "", fmt.Errorf("character self-registration is disabled on this server")
	}

	// Check invite tokens if configured
	if len(s.config.Server.InviteTokens) > 0 {
		validToken := false
		for _, token := range s.config.Server.InviteTokens {
			if token == req.InviteToken {
				validToken = true
				break
			}
		}
		if !validToken {
			return "", fmt.Errorf("invalid or missing invite token")
		}
	}

	characterID := strings.TrimSpace(req.ID)
	if !validID(characterID) {
		return "", fmt.Errorf("character id must contain only letters, numbers, underscores, and hyphens")
	}

	name := strings.TrimSpace(req.Name)
	occupation := strings.TrimSpace(req.Occupation)
	persona := strings.TrimSpace(req.PersonaPrompt)
	if name == "" || occupation == "" || persona == "" {
		return "", fmt.Errorf("character name, occupation, and persona_prompt are required")
	}

	locationID := strings.TrimSpace(req.LocationID)
	if locationID == "" {
		locationID = "market"
	}
	if !validID(locationID) {
		return "", fmt.Errorf("location id must contain only letters, numbers, underscores, and hyphens")
	}
	if err := requireExists(s.db, "locations", locationID); err != nil {
		return "", fmt.Errorf("invalid location: %w", err)
	}

	controllerType := strings.TrimSpace(req.ControllerType)
	if controllerType == "" {
		controllerType = "human"
	}
	if controllerType != "human" && controllerType != "agent" {
		return "", fmt.Errorf("controller_type must be either 'human' or 'agent'")
	}

	// Check if character already exists
	var exists int
	err := s.db.QueryRow("SELECT 1 FROM characters WHERE id = ?", characterID).Scan(&exists)
	if err == nil {
		return "", fmt.Errorf("character id %q is already taken", characterID)
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("check character id %q: %w", characterID, err)
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate character password: %w", err)
	}
	password := base64.RawURLEncoding.EncodeToString(secret)

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin character registration: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO characters (id, name, persona_prompt, occupation, current_location, controller_type, mqtt_password, gold, hunger, energy)
		VALUES (?, ?, ?, ?, ?, ?, ?, 15, 100, 100)
	`, characterID, name, persona, occupation, locationID, controllerType, password)
	if err != nil {
		return "", fmt.Errorf("register character %q: %w", characterID, err)
	}

	if err := db.InitializeCharacterProgression(tx, characterID, occupation); err != nil {
		return "", fmt.Errorf("initialize progression for character %q: %w", characterID, err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit character registration: %w", err)
	}

	log.Printf("[Server] Successfully self-registered character '%s' (%s, controller=%s)", characterID, name, controllerType)
	return password, nil
}

func (s *Server) handleRegisterAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("invalid JSON payload: %v", err),
		})
		return
	}

	password, err := s.RegisterCharacter(req)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		statusCode := http.StatusBadRequest
		if strings.Contains(err.Error(), "disabled") {
			statusCode = http.StatusForbidden
		} else if strings.Contains(err.Error(), "invite token") {
			statusCode = http.StatusUnauthorized
		} else if strings.Contains(err.Error(), "already taken") {
			statusCode = http.StatusConflict
		}
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(RegisterResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(RegisterResponse{
		Success:      true,
		CharacterID:  req.ID,
		MQTTPassword: password,
		Message:      "Character successfully registered. Save your mqtt_password securely to connect.",
	})
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, ch := range id {
		if (ch < 'a' || ch > 'z') &&
			(ch < 'A' || ch > 'Z') &&
			(ch < '0' || ch > '9') &&
			ch != '_' && ch != '-' {
			return false
		}
	}
	return true
}

func requireExists(database *sql.DB, table, id string) error {
	var exists int
	query := fmt.Sprintf("SELECT 1 FROM %s WHERE id = ?", table)
	if err := database.QueryRow(query, id).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s %q not found", strings.TrimSuffix(table, "s"), id)
		}
		return err
	}
	return nil
}
