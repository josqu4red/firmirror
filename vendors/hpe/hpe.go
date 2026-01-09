package hpe

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/criteo/firmirror/types"
	"github.com/criteo/firmirror/utils"
)

// HPEVendor implements the Vendor interface for HPE
type HPEVendor struct {
	BaseURL string
}

// HPEFirmwareEntry implements the FirmwareEntry interface for HPE
type HPEFirmwareEntry struct {
	Filename     string
	Entry        *HPECatalogEntry
	vendor       *HPEVendor // Reference to vendor for processing
	downloadPath string     // Store download path for processing
}

// NewHPEVendor creates a new HPE vendor instance
func NewHPEVendor(repo string) *HPEVendor {
	return &HPEVendor{
		BaseURL: "https://downloads.linux.hpe.com/SDR/repo/" + repo,
	}
}

// FetchCatalog implements the Vendor interface
func (hv *HPEVendor) FetchCatalog() (types.Catalog, error) {
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

// RetrieveFirmware implements the Vendor interface
func (hv *HPEVendor) RetrieveFirmware(entry types.FirmwareEntry, tmpDir string) (string, error) {
	hpeEntry, ok := entry.(*HPEFirmwareEntry)
	if !ok {
		return "", fmt.Errorf("invalid entry type for HPE vendor")
	}

	filepath, err := hv.fetchFirmware(hpeEntry.Filename, tmpDir)
	if err != nil {
		return "", err
	}

	// Store the download path in the entry for later processing
	hpeEntry.downloadPath = filepath
	return filepath, nil
}

// FirmwareToAppstream implements the Vendor interface
// HPE requires the firmware to be downloaded first, so we use the stored path
func (hv *HPEVendor) FirmwareToAppstream(entry types.FirmwareEntry) (*types.Component, error) {
	hpeEntry, ok := entry.(*HPEFirmwareEntry)
	if !ok {
		return nil, fmt.Errorf("invalid entry type for HPE vendor")
	}

	if hpeEntry.downloadPath == "" {
		return nil, fmt.Errorf("firmware must be retrieved first using RetrieveFirmware")
	}

	return processFirmware(hpeEntry.downloadPath)
}

// GetFilename implements the FirmwareEntry interface
func (hfe *HPEFirmwareEntry) GetFilename() string {
	return hfe.Filename
}

// ListEntries implements the Catalog interface for HPECatalog
func (hc *HPECatalog) ListEntries() []types.FirmwareEntry {
	entries := []types.FirmwareEntry{}
	for filename, catalogEntry := range hc.Entries {
		entry := catalogEntry // Create a copy to avoid pointer issues
		entries = append(entries, &HPEFirmwareEntry{
			Filename: filename,
			Entry:    &entry,
		})
	}
	return entries
}

func (hv *HPEVendor) fetchFirmware(filename, tmpDir string) (string, error) {
	fileurl := hv.BaseURL + "/current/" + filename
	file, err := utils.DownloadFile(fileurl)
	if err != nil {
		return "", err
	}
	defer file.Close()

	filepath := path.Join(tmpDir, path.Base(filename))
	err = utils.ReaderToFile(file, filepath)
	if err != nil {
		return "", err
	}

	return filepath, nil
}

func processFirmware(filename string) (*types.Component, error) {
	r, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	payloadFile, err := utils.GetFileFromName("payload.json", r)
	if err != nil {
		return nil, err
	}

	reader, err := payloadFile.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	byteValue, _ := io.ReadAll(reader)

	var payload HPEPayload

	err = json.Unmarshal(byteValue, &payload)
	if err != nil {
		return nil, err
	}

	appstream, err := buildAppStream(payload)
	if err != nil {
		return nil, err
	}

	appstream.Releases[0].Checksum = types.Checksum{
		Filename: filename,
		Target:   "content",
	}

	return appstream, nil
}

// buildAppStream converts an HPE firmware payload to an AppStream component.
// Note: we make the assumption that all devices in the payload will have the same version
// as well as the install duration.
func buildAppStream(fw HPEPayload) (*types.Component, error) {
	out := types.Component{
		Type:            "firmware",
		MetadataLicense: "proprietary",
		ProjectLicense:  "proprietary",
	}

	var devices []string
	for _, dev := range fw.Devices.Device {
		devices = append(devices, dev.DeviceName)
		// TODO:properly create GUID
		// deviceclass ?
		out.Provides = append(out.Provides, types.Firmware{
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
		out.Custom = append(out.Custom, types.Custom{
			Key:   "LVFS::DeviceFlags",
			Value: "skips-restart",
		})
		rebootMessage, err := getString(fw.Package.Installation.RebootDetails[0].Language, "en")
		if err != nil {
			return nil, err
		}

		out.Custom = append(out.Custom, types.Custom{
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
	out.Description = types.Description{
		Value: "<p>" + description + "</p>",
	}

	releaseDate, err := time.Parse("2006-01-02T15:04:05", fw.Package.ReleaseDate)
	if err != nil {
		return nil, err
	}

	out.Releases = append(out.Releases, types.Release{
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

	out.Custom = append(out.Custom, types.Custom{
		Key:   "LVFS::UpdateProtocol",
		Value: "org.dmtf.redfish",
	}, types.Custom{
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
