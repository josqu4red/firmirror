package hpe

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHPEVendor(t *testing.T) {
	repo := "test-repo"
	vendor := NewHPEVendor("/cache", repo)

	assert.NotNil(t, vendor, "Vendor should not be nil")
	expectedBaseURL := "https://downloads.linux.hpe.com/SDR/repo/test-repo"
	assert.Equal(t, expectedBaseURL, vendor.BaseURL, "BaseURL should be correctly constructed")
	assert.Equal(t, "/cache", vendor.CacheDir, "CacheDir should be set correctly")
}

func TestHPEVendor_FetchCatalog(t *testing.T) {
	server := mockServer(t)
	defer server.Close()

	// Create vendor with mock server URL
	vendor := &HPEVendor{
		BaseURL: server.URL,
	}

	catalog, err := vendor.FetchCatalog()
	assert.NoError(t, err, "FetchCatalog should not return an error")
	assert.NotNil(t, catalog, "Catalog should not be nil")

	hpeCatalog, ok := catalog.(*HPECatalog)
	assert.True(t, ok, "Catalog should be of type *HPECatalog")

	assert.Len(t, hpeCatalog.Entries, 2, "Catalog should contain exactly 2 entries")
	for filename := range hpeCatalog.Entries {
		assert.Equal(t, ".fwpkg", filepath.Ext(filename), "Only .fwpkg files should be included")
	}
}

func TestHPEVendor_ProcessFirmware(t *testing.T) {
	server := mockServer(t)
	defer server.Close()

	tmpCacheDir := t.TempDir()

	vendor := &HPEVendor{
		BaseURL:  server.URL,
		CacheDir: tmpCacheDir,
	}

	// Create a test firmware entry
	entry := &HPEFirmwareEntry{
		Filename: "test-firmware.fwpkg",
		Entry: &HPECatalogEntry{
			Date:        "20240115",
			Description: "Test firmware",
			Version:     "1.0.0",
		},
	}

	// Test processing firmware
	component, workDir, err := vendor.ProcessFirmware(entry)
	assert.NoError(t, err, "ProcessFirmware should not return an error")
	assert.NotNil(t, component, "Component should not be nil")
	assert.NotEmpty(t, workDir, "WorkDir should not be empty")

	// Verify work directory was created
	assert.DirExists(t, workDir, "Work directory should exist")

	// Verify the returned component
	assert.Equal(t, "firmware", component.Type, "Component type should be firmware")
	assert.Equal(t, "Network Device", component.Name, "Component name should match")
	assert.Equal(t, "Hewlett Packard Enterprise", component.DeveloperName, "Developer name should be HPE")

	// Verify releases
	assert.Len(t, component.Releases, 1, "Should have exactly one release")
	release := component.Releases[0]
	assert.Equal(t, "22.41.1000", release.Version, "Release version should match")
	assert.Equal(t, "2024-06-21", release.Date, "Release date should match")
	assert.Equal(t, 300, release.InstallDuration, "Install duration should match")
	assert.Equal(t, "test-firmware.fwpkg", release.Checksum.Filename, "Checksum filename should match")

	// Verify categories
	assert.Contains(t, component.Categories, "X-NetworkInterface", "Should contain X-NetworkInterface category")

	// Verify provides section
	assert.NotEmpty(t, component.Provides, "Should have provides entries")
}

func TestHPEVendor_ProcessFirmware_DownloadError(t *testing.T) {
	// Create server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	tmpCacheDir := t.TempDir()

	vendor := &HPEVendor{
		BaseURL:  server.URL,
		CacheDir: tmpCacheDir,
	}

	entry := &HPEFirmwareEntry{
		Filename: "nonexistent.fwpkg",
		Entry:    &HPECatalogEntry{},
	}

	_, _, err := vendor.ProcessFirmware(entry)
	assert.Error(t, err, "Should return error when download fails")
	assert.Contains(t, err.Error(), "failed to download", "Error should mention download failure")
}

func TestHPEVendor_ProcessFirmware_InvalidZip(t *testing.T) {
	// Create server that returns invalid zip content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not a valid zip file"))
	}))
	defer server.Close()

	tmpCacheDir := t.TempDir()

	vendor := &HPEVendor{
		BaseURL:  server.URL,
		CacheDir: tmpCacheDir,
	}

	entry := &HPEFirmwareEntry{
		Filename: "invalid.fwpkg",
		Entry:    &HPECatalogEntry{},
	}

	_, _, err := vendor.ProcessFirmware(entry)
	assert.Error(t, err, "Should return error for invalid zip")
}

func TestHPEVendor_ProcessFirmware_MissingPayload(t *testing.T) {
	// Create server that returns a zip without payload.json
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a zip without payload.json
		zipWriter := zip.NewWriter(w)
		fileWriter, _ := zipWriter.Create("other.txt")
		fileWriter.Write([]byte("not payload"))
		zipWriter.Close()
	}))
	defer server.Close()

	tmpCacheDir := t.TempDir()

	vendor := &HPEVendor{
		BaseURL:  server.URL,
		CacheDir: tmpCacheDir,
	}

	entry := &HPEFirmwareEntry{
		Filename: "no-payload.fwpkg",
		Entry:    &HPECatalogEntry{},
	}

	_, _, err := vendor.ProcessFirmware(entry)
	assert.Error(t, err, "Should return error when payload.json is missing")
	assert.Contains(t, err.Error(), "file not found", "Error should mention file not found")
}

func TestHPECatalog_ListEntries(t *testing.T) {
	// Create a test catalog
	catalog := &HPECatalog{
		Entries: map[string]HPECatalogEntry{
			"firmware1.fwpkg": {
				Date:        "2024-01-15",
				Description: "First firmware",
				Version:     "1.0.0",
			},
			"firmware2.fwpkg": {
				Date:        "2024-01-20",
				Description: "Second firmware",
				Version:     "2.0.0",
			},
		},
	}

	entries := catalog.ListEntries()
	assert.Len(t, entries, 2, "Should return exactly 2 entries")

	// Check that entries are properly converted
	filenames := make([]string, len(entries))
	for i, entry := range entries {
		hpeEntry, ok := entry.(*HPEFirmwareEntry)
		assert.True(t, ok, "Entry should be of type *HPEFirmwareEntry")
		assert.NotNil(t, hpeEntry.Entry, "Entry field should not be nil")

		filenames[i] = hpeEntry.GetFilename()
	}

	assert.Contains(t, filenames, "firmware1.fwpkg", "Should contain firmware1.fwpkg")
	assert.Contains(t, filenames, "firmware2.fwpkg", "Should contain firmware2.fwpkg")
}

func TestHPEFirmwareEntry_GetFilename(t *testing.T) {
	entry := &HPEFirmwareEntry{
		Filename: "test-firmware.fwpkg",
		Entry:    &HPECatalogEntry{},
	}

	filename := entry.GetFilename()
	assert.Equal(t, "test-firmware.fwpkg", filename, "GetFilename should return the correct filename")
}

// mockServer creates a test HTTP server that serves the test catalog
func mockServer(t *testing.T) *httptest.Server {
	// Create mock firmware files once at server creation
	tmpDir := t.TempDir()
	mockFirmwarePath := createMockHPEFirmware(t, tmpDir)

	mux := http.NewServeMux()

	// Serve the test catalog JSON
	mux.HandleFunc("/current/fwrepodata/fwrepo.json", func(w http.ResponseWriter, r *http.Request) {
		catalogPath := filepath.Join("testdata", "catalog.json")
		content, err := os.ReadFile(catalogPath)
		if !assert.NoError(t, err, "Should be able to read test catalog") {
			http.Error(w, "Test catalog not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(content)
	})

	// Serve mock firmware files - return actual zip file
	mux.HandleFunc("/current/", func(w http.ResponseWriter, r *http.Request) {
		filename := filepath.Base(r.URL.Path)

		// If requesting a .fwpkg file, serve the mock firmware zip
		if filepath.Ext(filename) == ".fwpkg" {
			content, err := os.ReadFile(mockFirmwarePath)
			if err != nil {
				http.Error(w, "Mock firmware not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			w.Write(content)
			return
		}

		// Default: return mock content
		mockContent := "Mock firmware content for " + filename
		w.Write([]byte(mockContent))
	})

	return httptest.NewServer(mux)
}

// Helper function to create a mock HPE firmware zip file with payload.json
func createMockHPEFirmware(t *testing.T, dir string) string {
	firmwarePath := filepath.Join(dir, "test-firmware.fwpkg")

	// Create zip file
	zipFile, err := os.Create(firmwarePath)
	assert.NoError(t, err, "Should be able to create zip file")
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	payloadJSON := filepath.Join("testdata", "payload.json")
	content, err := os.ReadFile(payloadJSON)
	assert.NoError(t, err, "Should be able to read test payload")

	payloadFile, err := zipWriter.Create("payload.json")
	assert.NoError(t, err, "Should be able to create payload.json in zip")

	_, err = payloadFile.Write(content)
	assert.NoError(t, err, "Should be able to write payload JSON")

	return firmwarePath
}
