package store

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAIModelTestStore(t *testing.T) *AIModelStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&AIModel{}); err != nil {
		t.Fatalf("migrate ai_models: %v", err)
	}
	return NewAIModelStore(db)
}

// Regression test for the {userID}_{provider}_{customModelName} id feature:
// a custom model name containing "/" must not leak into the primary key, and
// re-saving the same config must update in place instead of hitting a UNIQUE
// constraint violation.
func TestAIModelUpdateSanitizesSlashAndIsIdempotent(t *testing.T) {
	s := newAIModelTestStore(t)
	const uid = "user-1"

	// First save. The frontend sends the bare provider id when configuring from
	// the supported-models template.
	if err := s.Update(uid, "qwen", true, "", "", "qwen3/qwen37-plus", ""); err != nil {
		t.Fatalf("first save: %v", err)
	}

	models, err := s.List(uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model after first save, got %d", len(models))
	}
	m := models[0]
	if strings.Contains(m.ID, "/") {
		t.Fatalf("id must not contain a slash, got %q", m.ID)
	}
	if want := "user-1_qwen_qwen37-plus"; m.ID != want {
		t.Fatalf("id = %q, want %q", m.ID, want)
	}
	if m.Provider != "qwen" {
		t.Fatalf("provider = %q, want qwen", m.Provider)
	}
	if m.CustomModelName != "qwen3/qwen37-plus" {
		t.Fatalf("raw custom_model_name should be preserved, got %q", m.CustomModelName)
	}

	// Second save via the bare provider id again (this is the exact path that used
	// to create a duplicate record and fail with UNIQUE constraint failed).
	if err := s.Update(uid, "qwen", false, "", "", "qwen3/qwen37-plus", ""); err != nil {
		t.Fatalf("re-save via provider id should not error: %v", err)
	}
	models, _ = s.List(uid)
	if len(models) != 1 {
		t.Fatalf("re-save must update in place, got %d models", len(models))
	}
	if models[0].Enabled {
		t.Fatal("enabled should have been updated to false")
	}

	// Third save via the full stored id (exact-match path).
	if err := s.Update(uid, m.ID, true, "", "", "qwen3/qwen37-plus", ""); err != nil {
		t.Fatalf("re-save via full id should not error: %v", err)
	}
	models, _ = s.List(uid)
	if len(models) != 1 {
		t.Fatalf("re-save via full id must update in place, got %d models", len(models))
	}
	if !models[0].Enabled {
		t.Fatal("enabled should have been updated back to true")
	}
}

func TestAIModelUpdateCanReEnableAfterDeleteClearsFields(t *testing.T) {
	s := newAIModelTestStore(t)
	const uid = "user-1"
	const modelName = "qwen3/qwen37-plus"

	if err := s.Update(uid, "qwen", true, "secret", "https://dashscope.aliyuncs.com/compatible-mode/v1", modelName, ""); err != nil {
		t.Fatalf("first save: %v", err)
	}
	models, _ := s.List(uid)
	if len(models) != 1 {
		t.Fatalf("expected one model, got %d", len(models))
	}
	storedID := models[0].ID

	// Frontend delete disables the existing config and clears editable fields.
	if err := s.Update(uid, storedID, false, "", "", "", ""); err != nil {
		t.Fatalf("delete-style update: %v", err)
	}
	models, _ = s.List(uid)
	if len(models) != 1 {
		t.Fatalf("delete-style update should not create records, got %d", len(models))
	}
	if models[0].CustomModelName != modelName {
		t.Fatalf("disabled custom config should preserve natural key, got %q", models[0].CustomModelName)
	}

	if err := s.Update(uid, "qwen", true, "new-secret", "", modelName, ""); err != nil {
		t.Fatalf("re-enable same custom model should update existing row: %v", err)
	}
	models, _ = s.List(uid)
	if len(models) != 1 {
		t.Fatalf("re-enable should not insert a duplicate, got %d models", len(models))
	}
	if models[0].ID != storedID {
		t.Fatalf("expected same id %q, got %q", storedID, models[0].ID)
	}
	if !models[0].Enabled {
		t.Fatal("model should be enabled after re-add")
	}
}

// Two different custom model names on the same provider must produce two distinct
// configs (the "multiple configs per provider" feature).
func TestAIModelUpdateSupportsMultipleConfigsPerProvider(t *testing.T) {
	s := newAIModelTestStore(t)
	const uid = "user-1"

	if err := s.Update(uid, "qwen", true, "", "", "qwen3-plus", ""); err != nil {
		t.Fatalf("save qwen3-plus: %v", err)
	}
	if err := s.Update(uid, "qwen", true, "", "", "qwen3-max", ""); err != nil {
		t.Fatalf("save qwen3-max: %v", err)
	}

	models, _ := s.List(uid)
	if len(models) != 2 {
		t.Fatalf("expected 2 configs for provider qwen, got %d", len(models))
	}
	for _, m := range models {
		if m.Provider != "qwen" {
			t.Fatalf("provider = %q, want qwen", m.Provider)
		}
	}
}

// A config without a custom model name keeps the legacy {userID}_{provider} id and
// stays idempotent on re-save.
func TestAIModelUpdateLegacyFormatIsIdempotent(t *testing.T) {
	s := newAIModelTestStore(t)
	const uid = "user-1"

	if err := s.Update(uid, "deepseek", true, "", "", "", ""); err != nil {
		t.Fatalf("first save: %v", err)
	}
	models, _ := s.List(uid)
	if len(models) != 1 || models[0].ID != "user-1_deepseek" {
		t.Fatalf("expected single user-1_deepseek record, got %+v", models)
	}

	if err := s.Update(uid, "deepseek", false, "", "", "", ""); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	models, _ = s.List(uid)
	if len(models) != 1 {
		t.Fatalf("legacy re-save must update in place, got %d models", len(models))
	}
	if models[0].Enabled {
		t.Fatal("enabled should have been updated to false")
	}
}

func TestProviderFromModelID(t *testing.T) {
	cases := []struct {
		userID, id, want string
	}{
		{"user-1", "qwen", "qwen"},
		{"user-1", "user-1_qwen_qwen3-qwen37-plus", "qwen"},
		{"user-1", "user-1_deepseek", "deepseek"},
		{"default", "default_openai_gpt-4o", "openai"},
	}
	for _, c := range cases {
		if got := providerFromModelID(c.userID, c.id); got != c.want {
			t.Errorf("providerFromModelID(%q, %q) = %q, want %q", c.userID, c.id, got, c.want)
		}
	}
}

func TestSanitizeModelIDPart(t *testing.T) {
	cases := []struct{ in, want string }{
		{"qwen3/qwen37-plus", "qwen3-qwen37-plus"},
		{"a b:c", "a-b-c"},
		{"keep_dot.and-dash_", "keep_dot.and-dash_"},
	}
	for _, c := range cases {
		if got := sanitizeModelIDPart(c.in); got != c.want {
			t.Errorf("sanitizeModelIDPart(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildModelIDUsesPartAfterSlash(t *testing.T) {
	cases := []struct {
		userID, provider, customModelName, want string
	}{
		{"user-1", "qwen", "qwen3/qwen37-plus", "user-1_qwen_qwen37-plus"},
		{"user-1", "qwen", "qwen3.5-plus", "user-1_qwen_qwen3.5-plus"},
		{"user-1", "deepseek", "", "user-1_deepseek"},
		{"default", "openai", "openai/gpt-5.2", "default_openai_gpt-5.2"},
		{"u", "x", "a/b/c-model", "u_x_c-model"},
	}
	for _, c := range cases {
		if got := buildModelID(c.userID, c.provider, c.customModelName); got != c.want {
			t.Errorf("buildModelID(%q, %q, %q) = %q, want %q", c.userID, c.provider, c.customModelName, got, c.want)
		}
	}
}
