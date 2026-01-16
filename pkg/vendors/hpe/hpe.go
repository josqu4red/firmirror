package hpe

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/criteo/firmirror/pkg/firmirror"
	"github.com/criteo/firmirror/pkg/lvfs"
	"github.com/criteo/firmirror/pkg/utils"
)

// NewHPEVendor creates a new HPE vendor instance
func NewHPEVendor(cacheDir, repo string) *HPEVendor {
	return &HPEVendor{
		BaseURL:  "https://downloads.linux.hpe.com/SDR/repo/" + repo,
		CacheDir: cacheDir,
	}
}

// FetchCatalog implements the Vendor interface
func (hv *HPEVendor) FetchCatalog() (firmirror.Catalog, error) {
	catalog, err := hv.fetchCatalog()
	if err != nil {
		return nil, err
	}

	// Filter catalog entries based on vendor settings
	filteredCatalog := hv.filterCatalog(catalog)
	return filteredCatalog, nil
}

func (hv *HPEVendor) fetchCatalog() (*HPECatalog, error) {
	indexurl := hv.BaseURL + "/current/fwrepodata/fwrepo.json"
	jsondata, err := utils.DownloadFile(indexurl)
	if err != nil {
		return nil, err
	}
	defer jsondata.Close()

	var entries map[string]HPECatalogEntry
	if err := json.NewDecoder(jsondata).Decode(&entries); err != nil {
		return nil, err
	}

	catalog := &HPECatalog{
		Entries: entries,
	}
	return catalog, nil
}

func (hv *HPEVendor) filterCatalog(catalog *HPECatalog) *HPECatalog {
	filteredEntries := make(map[string]HPECatalogEntry)

	for filename, entry := range catalog.Entries {
		// Only include entries that end with .fwpkg
		if strings.HasSuffix(filename, ".fwpkg") {
			filteredEntries[filename] = entry
		}
	}

	filteredCatalog := &HPECatalog{
		Entries: filteredEntries,
	}
	return filteredCatalog
}

// ProcessFirmware implements the Vendor interface
func (hv *HPEVendor) ProcessFirmware(entry firmirror.FirmwareEntry) (*lvfs.Component, string, error) {
	hpeEntry, ok := entry.(*HPEFirmwareEntry)
	if !ok {
		return nil, "", fmt.Errorf("invalid entry type for HPE vendor")
	}

	// Create working directory for this firmware
	workDir := path.Join(hv.CacheDir, entry.GetFilename())
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, "", fmt.Errorf("failed to create work directory: %w", err)
	}

	// Download firmware to cache
	filepath := path.Join(workDir, path.Base(hpeEntry.Filename))
	if err := utils.DownloadFileToDest(hv.BaseURL+"/current/"+hpeEntry.Filename, filepath); err != nil {
		return nil, "", fmt.Errorf("failed to download firmware: %w", err)
	}

	// Read payload from ZIP
	payloadFile, err := readFileFromZip(filepath, "payload.json")
	if err != nil {
		return nil, "", fmt.Errorf("failed to read payload from ZIP: %w", err)
	}

	var payload HPEPayload
	if err = json.Unmarshal(payloadFile, &payload); err != nil {
		return nil, "", fmt.Errorf("failed to parse payload: %w", err)
	}

	// Convert to AppStream
	appstream, err := buildAppStream(payload)
	if err != nil {
		return nil, "", fmt.Errorf("failed to convert to AppStream: %w", err)
	}

	// Set checksum
	appstream.Releases[0].Checksum = lvfs.Checksum{
		Filename: hpeEntry.Filename,
		Target:   "content",
	}

	return appstream, workDir, nil
}

// ListEntries implements the Catalog interface for HPECatalog
func (hc *HPECatalog) ListEntries() []firmirror.FirmwareEntry {
	entries := []firmirror.FirmwareEntry{}
	for filename, catalogEntry := range hc.Entries {
		entry := catalogEntry // Create a copy to avoid pointer issues
		entries = append(entries, &HPEFirmwareEntry{
			Filename: filename,
			Entry:    &entry,
		})
	}
	return entries
}

// GetFilename implements the FirmwareEntry interface
func (hfe *HPEFirmwareEntry) GetFilename() string {
	return hfe.Filename
}

// buildAppStream converts an HPE firmware payload to an AppStream component.
// Note: we make the assumption that all devices in the payload will have the same version
// as well as the install duration.
func buildAppStream(fw HPEPayload) (*lvfs.Component, error) {
	out := lvfs.Component{
		Type:            "firmware",
		MetadataLicense: "proprietary",
		ProjectLicense:  "proprietary",
	}

	var devices []string
	for _, dev := range fw.Devices.Device {
		devices = append(devices, dev.DeviceName)
		// TODO:properly create GUID
		// deviceclass ?
		out.Provides = append(out.Provides, lvfs.Firmware{
			Type: "flashed",
			Text: dev.Target,
		})
	}
	slices.Sort(devices)
	devices = slices.Compact(devices)
	out.Name = strings.Join(devices[:], "/")

	manufacturer, err := getString(fw.Package.ManufacturerName, "en")
	if err != nil {
		return nil, err
	}
	out.DeveloperName = manufacturer
	out.ID = fmt.Sprintf("com.%s.%s", strings.ToLower(strings.ReplaceAll(manufacturer, " ", "")), strings.ReplaceAll(fw.Package.SwKeys[0].Name, " ", ""))

	if fw.Package.Installation.RebootRequired == "yes" {
		out.Custom = append(out.Custom, lvfs.Custom{
			Key:   "LVFS::DeviceFlags",
			Value: "skips-restart",
		})
		rebootMessage, err := getString(fw.Package.Installation.RebootDetails[0].Language, "en")
		if err != nil {
			return nil, err
		}

		out.Custom = append(out.Custom, lvfs.Custom{
			Key:   "LVFS::UpdateMessage",
			Value: rebootMessage,
		})
	}

	summary, err := getString(fw.Package.Name, "en")
	if err != nil {
		return nil, err
	}
	out.Summary = strings.ReplaceAll(strings.ReplaceAll(summary, "\t", ""), "  ", " ")

	description, err := getString(fw.Package.Description, "en")
	if err != nil {
		return nil, err
	}
	out.Description = lvfs.Description{
		Value: "<p>" + description + "</p>",
	}

	releaseDate, err := time.Parse("2006-01-02T15:04:05", fw.Package.ReleaseDate)
	if err != nil {
		return nil, err
	}

	out.Releases = append(out.Releases, lvfs.Release{
		Version:         fw.Devices.Device[0].Version,
		Date:            releaseDate.Format(time.DateOnly),
		InstallDuration: fw.Devices.Device[0].FirmwareImages[0].InstallDurationSec,
		Description:     out.Description,
	})

	for _, category := range fw.Package.Category {
		switch category.Key {
		case "2900095":
			// Firmware - Network
			out.Categories = append(out.Categories, "X-NetworkInterface")
		case "2900213":
			// Firmware - iLO
			out.Categories = append(out.Categories, "X-BaseboardManagementController")
		}
	}

	out.Custom = append(out.Custom, lvfs.Custom{
		Key:   "LVFS::UpdateProtocol",
		Value: "org.dmtf.redfish",
	}, lvfs.Custom{
		Key: "LVFS::DeviceIntegrity",
		// All fwpkg going through Redfish are signed
		Value: "signed",
	})

	return &out, nil
}

func getString(strings []HPETranslations, language string) (string, error) {
	for _, l := range strings {
		if l.Lang == language {
			return l.XLate, nil
		}
	}
	return "", fmt.Errorf("language not found: %s", language)
}

func readFileFromZip(zipFile, filename string) ([]byte, error) {
	archive, err := zip.OpenReader(zipFile)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	for _, f := range archive.File {
		if f.Name == filename {
			reader, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer reader.Close()

			return io.ReadAll(reader)
		}
	}
	return nil, fmt.Errorf("file not found: %s", filename)
}
