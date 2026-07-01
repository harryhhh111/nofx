package store

import (
	"errors"
	"fmt"
	"nofx/crypto"
	"nofx/logger"
	"strings"
	"time"

	"gorm.io/gorm"
)

// AIModelStore AI model storage
type AIModelStore struct {
	db *gorm.DB
}

// AIModel AI model configuration
type AIModel struct {
	ID              string                 `gorm:"primaryKey" json:"id"`
	UserID          string                 `gorm:"column:user_id;not null;default:default;index" json:"user_id"`
	Name            string                 `gorm:"not null" json:"name"`
	Provider        string                 `gorm:"not null" json:"provider"`
	Enabled         bool                   `gorm:"default:false" json:"enabled"`
	APIKey          crypto.EncryptedString `gorm:"column:api_key;default:''" json:"apiKey"`
	CustomAPIURL    string                 `gorm:"column:custom_api_url;default:''" json:"customApiUrl"`
	CustomModelName string                 `gorm:"column:custom_model_name;default:''" json:"customModelName"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

func (AIModel) TableName() string { return "ai_models" }

// NewAIModelStore creates a new AIModelStore
func NewAIModelStore(db *gorm.DB) *AIModelStore {
	return &AIModelStore{db: db}
}

func (s *AIModelStore) initTables() error {
	// For PostgreSQL with existing table, skip AutoMigrate
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'ai_models'`).Scan(&tableExists)
		if tableExists > 0 {
			return nil
		}
	}
	return s.db.AutoMigrate(&AIModel{})
}

func (s *AIModelStore) initDefaultData() error {
	// No longer pre-populate AI models - create on demand when user configures
	return nil
}

// List retrieves user's AI model list
func (s *AIModelStore) List(userID string) ([]*AIModel, error) {
	var models []*AIModel
	err := s.db.Where("user_id = ?", userID).Order("id").Find(&models).Error
	if err != nil {
		return nil, err
	}
	return models, nil
}

// Get retrieves a single AI model
func (s *AIModelStore) Get(userID, modelID string) (*AIModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	candidates := []string{}
	if userID != "" {
		candidates = append(candidates, userID)
	}
	if userID != "default" {
		candidates = append(candidates, "default")
	}
	if len(candidates) == 0 {
		candidates = append(candidates, "default")
	}

	for _, uid := range candidates {
		var model AIModel
		err := s.db.Where("user_id = ? AND id = ?", uid, modelID).First(&model).Error
		if err == nil {
			return &model, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// GetByID retrieves an AI model by ID only
func (s *AIModelStore) GetByID(modelID string) (*AIModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	var model AIModel
	err := s.db.Where("id = ?", modelID).First(&model).Error
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// GetDefault retrieves the default enabled AI model
func (s *AIModelStore) GetDefault(userID string) (*AIModel, error) {
	if userID == "" {
		userID = "default"
	}
	model, err := s.firstEnabled(userID)
	if err == nil {
		return model, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if userID != "default" {
		return s.firstEnabled("default")
	}
	return nil, fmt.Errorf("please configure an available AI model in the system first")
}

func (s *AIModelStore) firstEnabled(userID string) (*AIModel, error) {
	var model AIModel
	err := s.db.Where("user_id = ? AND enabled = ?", userID, true).
		Order("updated_at DESC, id ASC").
		First(&model).Error
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// GetAnyEnabled returns the first enabled AI model across all users.
// Used by single-user features (e.g. Telegram bot) that need any working LLM client.
func (s *AIModelStore) GetAnyEnabled() (*AIModel, error) {
	var model AIModel
	err := s.db.Where("enabled = ? AND api_key != ''", true).
		Order("updated_at DESC, id ASC").
		First(&model).Error
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// Update updates AI model, creates if not exists.
//
// The incoming id is either a bare provider name (e.g. "qwen", when configuring
// from the supported-models template) or a full stored id in the format
// {userID}_{provider}_{customModelName}. To stay robust regardless of which one
// the caller sends, matching is done in this order:
//  1. exact id match;
//  2. natural-key match on (userID, provider, customModelName) — this is what makes
//     re-saves idempotent instead of trying to INSERT a duplicate primary key;
//  3. legacy old-format id {userID}_{provider}.
//
// Only if none match do we create a new record, with a slash-free sanitized id.
//
// IMPORTANT: If apiKey is empty string, the existing API key will be preserved (not overwritten)
// IMPORTANT: If displayName is empty string, the existing name will be preserved (not overwritten)
func (s *AIModelStore) Update(userID, id string, enabled bool, apiKey, customAPIURL, customModelName, displayName string) error {
	// 1. Exact ID match first (caller passed a concrete stored id).
	var existingModel AIModel
	err := s.db.Where("user_id = ? AND id = ?", userID, id).First(&existingModel).Error
	if err == nil {
		return s.applyModelUpdate(&existingModel, enabled, apiKey, customAPIURL, customModelName, displayName)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	provider := providerFromModelID(userID, id)

	// 2. Idempotent match on the natural key so re-saving the same config updates
	//    in place instead of colliding on the primary key.
	err = s.db.Where("user_id = ? AND provider = ? AND custom_model_name = ?", userID, provider, customModelName).
		First(&existingModel).Error
	if err == nil {
		return s.applyModelUpdate(&existingModel, enabled, apiKey, customAPIURL, customModelName, displayName)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	// 3. Backward compatibility: legacy old-format id {userID}_{provider}.
	if id == provider {
		oldFormatID := fmt.Sprintf("%s_%s", userID, provider)
		err = s.db.Where("user_id = ? AND id = ?", userID, oldFormatID).First(&existingModel).Error
		if err == nil {
			return s.applyModelUpdate(&existingModel, enabled, apiKey, customAPIURL, customModelName, displayName)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}

	// 4. Create new record with a safe, slash-free id.
	name := s.resolveProviderName(provider)
	newModelID := buildModelID(userID, provider, customModelName)

	logger.Infof("✓ Creating new AI model configuration: ID=%s, Provider=%s, Name=%s", newModelID, provider, name)
	newModel := &AIModel{
		ID:              newModelID,
		UserID:          userID,
		Name:            name,
		Provider:        provider,
		Enabled:         enabled,
		APIKey:          crypto.EncryptedString(apiKey),
		CustomAPIURL:    customAPIURL,
		CustomModelName: customModelName,
	}
	return s.db.Create(newModel).Error
}

// applyModelUpdate applies a partial update to an existing model.
// apiKey / displayName are only overwritten when non-empty (preserve-on-empty semantics).
func (s *AIModelStore) applyModelUpdate(m *AIModel, enabled bool, apiKey, customAPIURL, customModelName, displayName string) error {
	updates := map[string]interface{}{
		"enabled":        enabled,
		"custom_api_url": customAPIURL,
		"updated_at":     time.Now().UTC(),
	}
	if enabled || customModelName != "" || m.CustomModelName == "" {
		updates["custom_model_name"] = customModelName
	}
	if apiKey != "" {
		updates["api_key"] = crypto.EncryptedString(apiKey)
	}
	if displayName != "" {
		updates["name"] = displayName
	}
	return s.db.Model(m).Updates(updates).Error
}

// resolveProviderName derives a display name for a provider, reusing an existing
// record's name when one exists.
func (s *AIModelStore) resolveProviderName(provider string) string {
	var refModel AIModel
	if err := s.db.Where("provider = ?", provider).First(&refModel).Error; err == nil {
		return refModel.Name
	}
	switch provider {
	case "deepseek":
		return "DeepSeek AI"
	case "qwen":
		return "Qwen AI"
	default:
		return provider + " AI"
	}
}

// providerFromModelID extracts the provider from a model id.
//
// A bare id (no {userID}_ prefix) is itself the provider name (e.g. "qwen").
// A full id is {userID}_{provider}_{customModelName}; the provider is the first
// segment after the userID prefix. The customModelName tail may contain "_" or
// "/", so we must NOT take the last "_"-separated segment (the pre-fix bug that
// mis-parsed provider as the model name).
func providerFromModelID(userID, id string) string {
	prefix := userID + "_"
	rest := id
	if strings.HasPrefix(id, prefix) {
		rest = strings.TrimPrefix(id, prefix)
	}
	if idx := strings.Index(rest, "_"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// buildModelID builds a stable primary-key id. If the customModelName contains
// a namespace prefix separated by "/" (e.g. "qwen/qwen3.5-plus"), only the
// model name after the last "/" is used in the id so the primary key stays
// short and URL-safe. The raw name is still stored in custom_model_name.
func buildModelID(userID, provider, customModelName string) string {
	if customModelName == "" {
		return fmt.Sprintf("%s_%s", userID, provider)
	}
	idPart := customModelName
	if idx := strings.LastIndex(customModelName, "/"); idx >= 0 {
		idPart = customModelName[idx+1:]
	}
	return fmt.Sprintf("%s_%s_%s", userID, provider, sanitizeModelIDPart(idPart))
}

// sanitizeModelIDPart keeps [A-Za-z0-9._-] and replaces every other character
// (slashes, spaces, colons, etc.) with "-".
func sanitizeModelIDPart(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// Create creates an AI model
func (s *AIModelStore) Create(userID, id, name, provider string, enabled bool, apiKey, customAPIURL string) error {
	model := &AIModel{
		ID:           id,
		UserID:       userID,
		Name:         name,
		Provider:     provider,
		Enabled:      enabled,
		APIKey:       crypto.EncryptedString(apiKey),
		CustomAPIURL: customAPIURL,
	}
	// Use FirstOrCreate to ignore if already exists
	return s.db.Where("id = ?", id).FirstOrCreate(model).Error
}
