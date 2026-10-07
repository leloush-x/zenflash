package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"zenflash-llm/internal/cline/cline"
)

type ModelStatus string

const (
	ModelActive  ModelStatus = "active"
	ModelRemoved ModelStatus = "removed"
	ModelUnknown ModelStatus = "unknown"
)

type ModelInfo struct {
	ID             string      `json:"id"`
	Name           string      `json:"name,omitempty"`
	Description    string      `json:"description,omitempty"`
	Tags           []string    `json:"tags,omitempty"`
	Source         string      `json:"source"`
	Provider       string      `json:"provider"`
	Cost           string      `json:"cost"`
	Status         ModelStatus `json:"status"`
	RequiresStream bool        `json:"requiresStream,omitempty"`
	SyncedAt       time.Time   `json:"syncedAt,omitempty"`
}

var (
	modelsMu       sync.Mutex
	modelsCache    map[string]*ModelInfo
	modelsSyncing  bool
	modelsLastSync time.Time
)

const (
	modelsRefreshInterval = 60 * time.Second
	modelsSyncTimeout     = 25 * time.Second
)

const recommendedModelsURL = cline.ClineAPIBase + "/ai/cline/recommended-models"

// publicModelsURL is the unauthenticated OpenAI-compatible model list. It
// carries no pricing field; free models are identified dynamically by the
// upstream ":free" / "-free" naming rule (see isPublicFreeModel).
const publicModelsURL = cline.ClineAPIBase + "/models"

// isPublicFreeModel reports whether an id from the public model list is a
// free-tier model. Rule-based, not a hardcoded list: upstream marks free
// models with a ":free" (or "-free") suffix.
func isPublicFreeModel(id string) bool {
	return strings.HasSuffix(id, ":free") || strings.HasSuffix(id, "-free")
}

func initModelsCache() {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	if modelsCache != nil {
		return
	}
	// No hardcoded seeds: the cache fills only from upstream syncs
	// (authenticated recommended-models + public /models list).
	modelsCache = make(map[string]*ModelInfo)
}

// mergePublicModels folds every id from the public list into the cache.
// Free-ness stays a dynamic naming rule (isPublicFreeModel): free ids get
// Cost "free", the rest keep Cost "" (the public list carries no pricing).
// Returns the number of newly added ids.
func mergePublicModels(ids []string) int {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	added := 0
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		free := isPublicFreeModel(id)
		if cached, ok := modelsCache[id]; ok {
			if free {
				cached.Source = "free"
				cached.Cost = "free"
			}
			cached.Status = ModelActive
			cached.SyncedAt = time.Now()
			continue
		}
		provider := id
		if i := indexByte(id, '/'); i >= 0 {
			provider = id[:i]
		}
		m := &ModelInfo{
			ID:             id,
			Source:         "public",
			Provider:       provider,
			Status:         ModelActive,
			RequiresStream: indexByte(id, ':') < 0,
			SyncedAt:       time.Now(),
		}
		if free {
			m.Source = "free"
			m.Cost = "free"
		}
		modelsCache[id] = m
		added++
	}
	return added
}

func sortedModelSlice(in []*ModelInfo) []*ModelInfo {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0; j-- {
			if in[j-1].ID < in[j].ID {
				break
			}
			in[j-1], in[j] = in[j], in[j-1]
		}
	}
	return in
}

func getAllModels() []*ModelInfo {
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()
	out := make([]*ModelInfo, 0, len(modelsCache))
	for _, m := range modelsCache {
		cp := *m
		out = append(out, &cp)
	}
	return sortedModelSlice(out)
}

func getFreeModels() []*ModelInfo {
	all := getAllModels()
	out := all[:0]
	for _, m := range all {
		if m.Cost == "free" {
			out = append(out, m)
		}
	}
	return out
}

type recommendedPayload struct {
	Free []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	} `json:"free"`
}

func syncRecommendedModels() (int, error) {
	initModelsCache()

	acc := pickAccount()
	if acc == nil {
		return 0, fmt.Errorf("no active accounts")
	}
	token, err := ensureAccountToken(acc)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("GET", recommendedModelsURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header = clineHeaders(token, "")
	req.Header.Set("X-Task-ID", fmt.Sprintf("sess_sync_%d", time.Now().UnixMilli()))

	client := &http.Client{Timeout: modelsSyncTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var payload recommendedPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}

	modelsMu.Lock()
	defer modelsMu.Unlock()

	added := 0
	for _, m := range payload.Free {
		id := m.ID
		provider := id
		if i := indexByte(id, '/'); i >= 0 {
			provider = id[:i]
		}
		if cached, ok := modelsCache[id]; ok {
			cached.Source = "free"
			cached.Cost = "free"
			cached.Provider = provider
			cached.Status = ModelActive
			cached.SyncedAt = time.Now()
			if cached.Name == "" {
				cached.Name = m.Name
			}
			if cached.Description == "" {
				cached.Description = m.Description
			}
			if len(cached.Tags) == 0 && len(m.Tags) > 0 {
				cached.Tags = append([]string(nil), m.Tags...)
			}
			continue
		}
		modelsCache[id] = &ModelInfo{
			ID:             id,
			Name:           m.Name,
			Description:    m.Description,
			Tags:           append([]string(nil), m.Tags...),
			Source:         "free",
			Provider:       provider,
			Cost:           "free",
			Status:         ModelActive,
			RequiresStream: indexByte(id, ':') < 0,
			SyncedAt:       time.Now(),
		}
		added++
	}

	modelsLastSync = time.Now()
	return added, nil
}

// syncPublicModels merges the unauthenticated public model list into the
// cache using the dynamic free-naming rule. It needs no login, so free
// models are visible pre-login and when the authenticated feed fails.
func syncPublicModels() (int, error) {
	initModelsCache()
	req, err := http.NewRequest("GET", publicModelsURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Task-ID", fmt.Sprintf("sess_sync_%d", time.Now().UnixMilli()))
	client := &http.Client{Timeout: modelsSyncTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		ids = append(ids, m.ID)
	}
	added := mergePublicModels(ids)
	modelsMu.Lock()
	modelsLastSync = time.Now()
	modelsMu.Unlock()
	return added, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func syncModelsOnce() {
	initModelsCache()
	modelsMu.Lock()
	if modelsSyncing {
		modelsMu.Unlock()
		return
	}
	modelsSyncing = true
	modelsMu.Unlock()
	defer func() {
		modelsMu.Lock()
		modelsSyncing = false
		modelsMu.Unlock()
	}()

	if added, err := syncRecommendedModels(); err != nil {
		log.Printf("  model sync: recommended feed failed (%v)", err)
	} else if added > 0 {
		log.Printf("  model sync: %d new free models from recommended feed", added)
	}
	if added, err := syncPublicModels(); err != nil {
		log.Printf("  model sync: public feed failed (%v)", err)
	} else if added > 0 {
		log.Printf("  model sync: %d new free models from public feed", added)
	} else {
		log.Printf("  model sync: %d free models up to date", len(getFreeModels()))
	}
}

func getDefaultModel() string {
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()

	if defaultModel != "" {
		if m, ok := modelsCache[defaultModel]; ok && m.Status == ModelActive {
			return defaultModel
		}
	}
	// Prefer a free model as the automatic default; fall back to any known
	// id only when no free model has synced yet. Never invent a hardcoded id.
	for _, m := range modelsCache {
		if m.Status == ModelActive && m.Cost == "free" {
			return m.ID
		}
	}
	for _, m := range modelsCache {
		if m.Status == ModelActive {
			return m.ID
		}
	}
	return defaultModel
}

func normalizeRequestModel(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return getDefaultModel()
	}
	// Dynamic: pass unknown ids through untouched so newly rotated upstream
	// models work before the next sync learns them.
	return id
}

func apiModelList() []map[string]any {
	out := make([]map[string]any, 0, len(modelsCache))
	for _, m := range getAllModels() {
		out = append(out, map[string]any{
			"id":             m.ID,
			"object":         "model",
			"created":        time.Now().UnixMilli(),
			"owned_by":       m.Provider,
			"source":         m.Source,
			"status":         m.Status,
			"cost":           m.Cost,
			"requiresStream": m.RequiresStream,
			"syncedAt":       m.SyncedAt,
		})
	}
	return out
}

func ensureModelsFresh() {
	initModelsCache()
	modelsMu.Lock()
	needSync := modelsLastSync.IsZero() || time.Since(modelsLastSync) > modelsRefreshInterval
	syncing := modelsSyncing
	modelsMu.Unlock()
	if needSync && !syncing {
		go syncModelsOnce()
	}
}

func startModelsRefresher() {
	go func() {
		syncModelsOnce()
		ticker := time.NewTicker(modelsRefreshInterval)
		for range ticker.C {
			syncModelsOnce()
		}
	}()
}

// ClineModelIDs returns the sorted IDs currently in the embedded Cline free
// model cache. Read-only: it never triggers a sync and never changes pool
// behavior. The gateway uses it to advertise the Cline tier separately from
// OpenCode Go.
func ClineModelIDs() []string {
	models := getAllModels()
	out := make([]string, 0, len(models))
	for _, m := range models {
		if m != nil && m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

// ClineModelDetails returns a read-only snapshot of the embedded Cline pool
// with display metadata. Used to enrich the gateway Cline tier without
// changing pool behavior.
func ClineModelDetails() []ModelInfo {
	models := getAllModels()
	out := make([]ModelInfo, 0, len(models))
	for _, m := range models {
		if m == nil || m.ID == "" {
			continue
		}
		cp := *m
		cp.Tags = append([]string(nil), m.Tags...)
		out = append(out, cp)
	}
	return out
}
