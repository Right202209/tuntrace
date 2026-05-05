package settings

import (
	"context"
	"os"
	"strings"
	"sync"

	"tuntrace/internal/store"
)

const (
	KeyMihomoURL    = "mihomo_url"
	KeyMihomoSecret = "mihomo_secret"

	EnvMihomoURL    = "MIHOMO_URL"
	EnvMihomoSecret = "MIHOMO_SECRET"
)

type Mihomo struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

type Manager struct {
	store *store.Store
	mu    sync.RWMutex
	cur   Mihomo
}

func NewManager(st *store.Store) *Manager {
	return &Manager{store: st}
}

// Load resolves Mihomo settings: env > DB. Env values override and persist to DB
// so a later run without env vars still finds the same values.
func (m *Manager) Load(ctx context.Context) (Mihomo, error) {
	dbURL, err := m.store.SettingGet(ctx, KeyMihomoURL)
	if err != nil {
		return Mihomo{}, err
	}
	dbSecret, err := m.store.SettingGet(ctx, KeyMihomoSecret)
	if err != nil {
		return Mihomo{}, err
	}

	eff := Mihomo{URL: dbURL, Secret: dbSecret}

	if envURL := strings.TrimSpace(os.Getenv(EnvMihomoURL)); envURL != "" {
		eff.URL = envURL
		if dbURL != envURL {
			if err := m.store.SettingSet(ctx, KeyMihomoURL, envURL); err != nil {
				return Mihomo{}, err
			}
		}
	}
	if envSecret, ok := os.LookupEnv(EnvMihomoSecret); ok {
		eff.Secret = envSecret
		if dbSecret != envSecret {
			if err := m.store.SettingSet(ctx, KeyMihomoSecret, envSecret); err != nil {
				return Mihomo{}, err
			}
		}
	}

	m.mu.Lock()
	m.cur = eff
	m.mu.Unlock()
	return eff, nil
}

func (m *Manager) Get() Mihomo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

func (m *Manager) Save(ctx context.Context, s Mihomo) error {
	url := strings.TrimSpace(s.URL)
	if err := m.store.SettingSet(ctx, KeyMihomoURL, url); err != nil {
		return err
	}
	if err := m.store.SettingSet(ctx, KeyMihomoSecret, s.Secret); err != nil {
		return err
	}
	m.mu.Lock()
	m.cur = Mihomo{URL: url, Secret: s.Secret}
	m.mu.Unlock()
	return nil
}
