package database

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListServers_WithNilValue tests that ListServers handles records with nil Value pointers gracefully
func TestListServers_WithNilValue(t *testing.T) {
	ctx := context.Background()

	// Create a temporary JSON file with records that have nil values
	tmpFile, err := os.CreateTemp("", "registry-test-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// Create test data with one valid record and one with nil Value
	validServer := &apiv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
		Name:        "io.github.test/valid-server",
		Description: "A valid server",
		Version:     "1.0.0",
		Repository: &model.Repository{
			URL:    "https://github.com/test/valid",
			Source: "github",
		},
	}

	testData := jsonFileData{
		Servers: []serverRecord{
			{
				ServerName:  "io.github.test/valid-server",
				Version:     "1.0.0",
				Status:      string(model.StatusActive),
				PublishedAt: time.Now(),
				UpdatedAt:   time.Now(),
				IsLatest:    true,
				Value:       validServer,
			},
			{
				// This record has nil Value - simulating corrupted/incompatible data
				ServerName:  "io.github.test/corrupted-server",
				Version:     "1.0.0",
				Status:      string(model.StatusActive),
				PublishedAt: time.Now(),
				UpdatedAt:   time.Now(),
				IsLatest:    true,
				Value:       nil,
			},
		},
	}

	// Write test data to file
	data, err := json.Marshal(testData)
	require.NoError(t, err)
	_, err = tmpFile.Write(data)
	require.NoError(t, err)
	tmpFile.Close()

	// Create database instance
	db, err := NewJSONFileDB(ctx, tmpFile.Name())
	require.NoError(t, err)

	// Test ListServers - should return only the valid record, skipping the nil one
	results, nextCursor, err := db.ListServers(ctx, nil, nil, "", 100)
	require.NoError(t, err)
	assert.Empty(t, nextCursor, "No cursor should be returned for small result set")
	assert.Len(t, results, 1, "Should return only 1 valid record, skipping the nil one")
	assert.Equal(t, "io.github.test/valid-server", results[0].Server.Name)
}

// TestListServers_WithRemoteURLFilter_NilValue tests that RemoteURL filter handles nil Values
func TestListServers_WithRemoteURLFilter_NilValue(t *testing.T) {
	ctx := context.Background()

	tmpFile, err := os.CreateTemp("", "registry-test-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// Create a valid server with remotes
	remoteURL := "https://mcp.example.com/server"
	validServer := &apiv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
		Name:        "io.github.test/remote-server",
		Description: "A server with remotes",
		Version:     "1.0.0",
		Repository: &model.Repository{
			URL:    "https://github.com/test/remote",
			Source: "github",
		},
		Remotes: []model.Transport{
			{
				Type: "streamable-http",
				URL:  remoteURL,
			},
		},
	}

	testData := jsonFileData{
		Servers: []serverRecord{
			{
				ServerName:  "io.github.test/remote-server",
				Version:     "1.0.0",
				Status:      string(model.StatusActive),
				PublishedAt: time.Now(),
				UpdatedAt:   time.Now(),
				IsLatest:    true,
				Value:       validServer,
			},
			{
				// Corrupted record with nil Value
				ServerName:  "io.github.test/corrupted-server",
				Version:     "1.0.0",
				Status:      string(model.StatusActive),
				PublishedAt: time.Now(),
				UpdatedAt:   time.Now(),
				IsLatest:    true,
				Value:       nil,
			},
		},
	}

	data, err := json.Marshal(testData)
	require.NoError(t, err)
	_, err = tmpFile.Write(data)
	require.NoError(t, err)
	tmpFile.Close()

	db, err := NewJSONFileDB(ctx, tmpFile.Name())
	require.NoError(t, err)

	// Test with RemoteURL filter - should not crash and should find the valid server
	filter := &ServerFilter{
		RemoteURL: &remoteURL,
	}
	results, _, err := db.ListServers(ctx, nil, filter, "", 100)
	require.NoError(t, err)
	assert.Len(t, results, 1, "Should find 1 server with matching remote URL")
	assert.Equal(t, "io.github.test/remote-server", results[0].Server.Name)
}
