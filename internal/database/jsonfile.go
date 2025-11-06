package database

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// JSONFileDB implements the Database interface using a local JSON file
type JSONFileDB struct {
	filePath string
	mu       sync.RWMutex
	data     *jsonFileData
	locks    map[uint64]*sync.Mutex // advisory locks by server name hash
	locksMu  sync.Mutex
}

// jsonFileData represents the structure stored in the JSON file
type jsonFileData struct {
	Servers []serverRecord `json:"servers"`
}

// serverRecord represents a single server version in storage
type serverRecord struct {
	ServerName  string                    `json:"server_name"`
	Version     string                    `json:"version"`
	Status      string                    `json:"status"`
	PublishedAt time.Time                 `json:"published_at"`
	UpdatedAt   time.Time                 `json:"updated_at"`
	IsLatest    bool                      `json:"is_latest"`
	Value       *apiv0.ServerJSON         `json:"value"`
	Meta        *apiv0.RegistryExtensions `json:"meta,omitempty"`
}

// jsonTx is a mock transaction type for JSON file database
type jsonTx struct {
	db         *JSONFileDB
	committed  bool
	rolledBack bool
	locks      []*sync.Mutex
}

// NewJSONFileDB creates a new JSON file-based database
func NewJSONFileDB(ctx context.Context, filePath string) (*JSONFileDB, error) {
	db := &JSONFileDB{
		filePath: filePath,
		data:     &jsonFileData{Servers: []serverRecord{}},
		locks:    make(map[uint64]*sync.Mutex),
	}

	// Try to load existing data
	if _, err := os.Stat(filePath); err == nil {
		if err := db.load(); err != nil {
			return nil, fmt.Errorf("failed to load existing data: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check file: %w", err)
	}

	return db, nil
}

// load reads data from the JSON file
func (db *JSONFileDB) load() error {
	data, err := os.ReadFile(db.filePath)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return nil
	}

	var fileData jsonFileData
	if err := json.Unmarshal(data, &fileData); err != nil {
		return err
	}

	db.data = &fileData
	return nil
}

// Reload reloads data from the JSON file (thread-safe)
func (db *JSONFileDB) Reload() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.load()
}

// save writes data to the JSON file
func (db *JSONFileDB) save() error {
	data, err := json.MarshalIndent(db.data, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file first, then rename (atomic on most systems)
	tempFile := db.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}

	return os.Rename(tempFile, db.filePath)
}

// CreateServer implements Database.CreateServer
func (db *JSONFileDB) CreateServer(ctx context.Context, tx pgx.Tx, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Check if version already exists
	for _, record := range db.data.Servers {
		if record.ServerName == serverJSON.Name && record.Version == serverJSON.Version {
			return nil, ErrAlreadyExists
		}
	}

	now := time.Now()
	if officialMeta == nil {
		officialMeta = &apiv0.RegistryExtensions{
			Status:      model.StatusActive,
			PublishedAt: now,
			UpdatedAt:   now,
			IsLatest:    true,
		}
	}

	record := serverRecord{
		ServerName:  serverJSON.Name,
		Version:     serverJSON.Version,
		Status:      string(officialMeta.Status),
		PublishedAt: officialMeta.PublishedAt,
		UpdatedAt:   officialMeta.UpdatedAt,
		IsLatest:    officialMeta.IsLatest,
		Value:       serverJSON,
		Meta:        officialMeta,
	}

	db.data.Servers = append(db.data.Servers, record)

	if err := db.save(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
	}

	return &apiv0.ServerResponse{
		Server: *serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: officialMeta,
		},
	}, nil
}

// UpdateServer implements Database.UpdateServer
func (db *JSONFileDB) UpdateServer(ctx context.Context, tx pgx.Tx, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	for i := range db.data.Servers {
		if db.data.Servers[i].ServerName == serverName && db.data.Servers[i].Version == version {
			db.data.Servers[i].Value = serverJSON
			db.data.Servers[i].UpdatedAt = time.Now()

			if err := db.save(); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
			}

			return &apiv0.ServerResponse{
				Server: *serverJSON,
				Meta: apiv0.ResponseMeta{
					Official: &apiv0.RegistryExtensions{
						Status:      model.Status(db.data.Servers[i].Status),
						PublishedAt: db.data.Servers[i].PublishedAt,
						UpdatedAt:   db.data.Servers[i].UpdatedAt,
						IsLatest:    db.data.Servers[i].IsLatest,
					},
				},
			}, nil
		}
	}

	return nil, ErrNotFound
}

// SetServerStatus implements Database.SetServerStatus
func (db *JSONFileDB) SetServerStatus(ctx context.Context, tx pgx.Tx, serverName, version string, status string) (*apiv0.ServerResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	for i := range db.data.Servers {
		if db.data.Servers[i].ServerName == serverName && db.data.Servers[i].Version == version {
			db.data.Servers[i].Status = status
			db.data.Servers[i].UpdatedAt = time.Now()

			if err := db.save(); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
			}

			return &apiv0.ServerResponse{
				Server: *db.data.Servers[i].Value,
				Meta: apiv0.ResponseMeta{
					Official: &apiv0.RegistryExtensions{
						Status:      model.Status(db.data.Servers[i].Status),
						PublishedAt: db.data.Servers[i].PublishedAt,
						UpdatedAt:   db.data.Servers[i].UpdatedAt,
						IsLatest:    db.data.Servers[i].IsLatest,
					},
				},
			}, nil
		}
	}

	return nil, ErrNotFound
}

// ListServers implements Database.ListServers
func (db *JSONFileDB) ListServers(ctx context.Context, tx pgx.Tx, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var results []*apiv0.ServerResponse
	var startIndex int

	// Handle cursor
	if cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) == 2 {
			cursorName, cursorVersion := parts[0], parts[1]
			for i, record := range db.data.Servers {
				if record.ServerName == cursorName && record.Version == cursorVersion {
					startIndex = i + 1
					break
				}
			}
		}
	}

	// Filter and collect results
	for i := startIndex; i < len(db.data.Servers); i++ {
		record := db.data.Servers[i]

		// Apply filters
		if filter != nil {
			if filter.Name != nil && record.ServerName != *filter.Name {
				continue
			}
			if filter.Version != nil && record.Version != *filter.Version {
				continue
			}
			if filter.IsLatest != nil && record.IsLatest != *filter.IsLatest {
				continue
			}
			if filter.SubstringName != nil && !strings.Contains(strings.ToLower(record.ServerName), strings.ToLower(*filter.SubstringName)) {
				continue
			}
			if filter.UpdatedSince != nil && !record.UpdatedAt.After(*filter.UpdatedSince) {
				continue
			}
			if filter.RemoteURL != nil {
				found := false
				for _, remote := range record.Value.Remotes {
					if remote.URL == *filter.RemoteURL {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
		}

		results = append(results, &apiv0.ServerResponse{
			Server: *record.Value,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:      model.Status(record.Status),
					PublishedAt: record.PublishedAt,
					UpdatedAt:   record.UpdatedAt,
					IsLatest:    record.IsLatest,
				},
			},
		})

		if len(results) >= limit {
			break
		}
	}

	// Generate next cursor
	var nextCursor string
	if len(results) == limit && startIndex+len(results) < len(db.data.Servers) {
		lastRecord := db.data.Servers[startIndex+len(results)-1]
		nextCursor = fmt.Sprintf("%s:%s", lastRecord.ServerName, lastRecord.Version)
	}

	return results, nextCursor, nil
}

// GetServerByName implements Database.GetServerByName (returns latest version)
func (db *JSONFileDB) GetServerByName(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	for _, record := range db.data.Servers {
		if record.ServerName == serverName && record.IsLatest {
			return &apiv0.ServerResponse{
				Server: *record.Value,
				Meta: apiv0.ResponseMeta{
					Official: &apiv0.RegistryExtensions{
						Status:      model.Status(record.Status),
						PublishedAt: record.PublishedAt,
						UpdatedAt:   record.UpdatedAt,
						IsLatest:    record.IsLatest,
					},
				},
			}, nil
		}
	}

	return nil, ErrNotFound
}

// GetServerByNameAndVersion implements Database.GetServerByNameAndVersion
func (db *JSONFileDB) GetServerByNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string) (*apiv0.ServerResponse, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	for _, record := range db.data.Servers {
		if record.ServerName == serverName && record.Version == version {
			return &apiv0.ServerResponse{
				Server: *record.Value,
				Meta: apiv0.ResponseMeta{
					Official: &apiv0.RegistryExtensions{
						Status:      model.Status(record.Status),
						PublishedAt: record.PublishedAt,
						UpdatedAt:   record.UpdatedAt,
						IsLatest:    record.IsLatest,
					},
				},
			}, nil
		}
	}

	return nil, ErrNotFound
}

// GetAllVersionsByServerName implements Database.GetAllVersionsByServerName
func (db *JSONFileDB) GetAllVersionsByServerName(ctx context.Context, tx pgx.Tx, serverName string) ([]*apiv0.ServerResponse, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var results []*apiv0.ServerResponse
	for _, record := range db.data.Servers {
		if record.ServerName == serverName {
			results = append(results, &apiv0.ServerResponse{
				Server: *record.Value,
				Meta: apiv0.ResponseMeta{
					Official: &apiv0.RegistryExtensions{
						Status:      model.Status(record.Status),
						PublishedAt: record.PublishedAt,
						UpdatedAt:   record.UpdatedAt,
						IsLatest:    record.IsLatest,
					},
				},
			})
		}
	}

	// Sort by published_at descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Meta.Official.PublishedAt.After(results[j].Meta.Official.PublishedAt)
	})

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	return results, nil
}

// GetCurrentLatestVersion implements Database.GetCurrentLatestVersion
func (db *JSONFileDB) GetCurrentLatestVersion(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	return db.GetServerByName(ctx, tx, serverName)
}

// CountServerVersions implements Database.CountServerVersions
func (db *JSONFileDB) CountServerVersions(ctx context.Context, tx pgx.Tx, serverName string) (int, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	count := 0
	for _, record := range db.data.Servers {
		if record.ServerName == serverName {
			count++
		}
	}

	return count, nil
}

// CheckVersionExists implements Database.CheckVersionExists
func (db *JSONFileDB) CheckVersionExists(ctx context.Context, tx pgx.Tx, serverName, version string) (bool, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	for _, record := range db.data.Servers {
		if record.ServerName == serverName && record.Version == version {
			return true, nil
		}
	}

	return false, nil
}

// UnmarkAsLatest implements Database.UnmarkAsLatest
func (db *JSONFileDB) UnmarkAsLatest(ctx context.Context, tx pgx.Tx, serverName string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	found := false
	for i := range db.data.Servers {
		if db.data.Servers[i].ServerName == serverName && db.data.Servers[i].IsLatest {
			db.data.Servers[i].IsLatest = false
			found = true
		}
	}

	if !found {
		return nil // Not an error, just nothing to do
	}

	return db.save()
}

// AcquirePublishLock implements Database.AcquirePublishLock
func (db *JSONFileDB) AcquirePublishLock(ctx context.Context, tx pgx.Tx, serverName string) error {
	// Generate lock ID using same hash algorithm as PostgreSQL version
	h := fnv.New64a()
	h.Write([]byte(serverName))
	lockID := h.Sum64()

	db.locksMu.Lock()
	if _, exists := db.locks[lockID]; !exists {
		db.locks[lockID] = &sync.Mutex{}
	}
	lock := db.locks[lockID]
	db.locksMu.Unlock()

	// Acquire the lock (will block if already held)
	lock.Lock()

	// Store the lock in the transaction context so we can release it later
	if jtx, ok := tx.(*jsonTx); ok {
		jtx.addLock(lock)
	}

	return nil
}

// InTransaction implements Database.InTransaction
func (db *JSONFileDB) InTransaction(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx := &jsonTx{
		db:    db,
		locks: make([]*sync.Mutex, 0),
	}

	defer func() {
		// Release all locks acquired during the transaction
		for _, lock := range tx.locks {
			lock.Unlock()
		}
	}()

	err := fn(ctx, tx)
	if err != nil {
		tx.rolledBack = true
		return err
	}

	tx.committed = true
	return nil
}

// Close implements Database.Close
func (db *JSONFileDB) Close() error {
	// Final save on close
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.save()
}

// addLock adds a lock to the transaction's list of held locks
func (tx *jsonTx) addLock(lock *sync.Mutex) {
	tx.locks = append(tx.locks, lock)
}

// Mock methods to satisfy pgx.Tx interface (these won't be called in practice)
func (tx *jsonTx) Begin(ctx context.Context) (pgx.Tx, error)   { return nil, nil }
func (tx *jsonTx) Commit(ctx context.Context) error            { return nil }
func (tx *jsonTx) Rollback(ctx context.Context) error          { return nil }
func (tx *jsonTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *jsonTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (tx *jsonTx) LargeObjects() pgx.LargeObjects                                { return pgx.LargeObjects{} }
func (tx *jsonTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (tx *jsonTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (tx *jsonTx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return nil, nil
}
func (tx *jsonTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return nil
}
func (tx *jsonTx) Conn() *pgx.Conn { return nil }
