package firmirror

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path"

	"github.com/criteo/firmirror/pkg/lvfs"
)

type FirmirrorConfig struct {
	OutputDir string // Directory for final CAB files and metadata
	CacheDir  string // Cache directory for temporary files and original firmware
}

type FimirrorSyncer struct {
	Config  FirmirrorConfig
	vendors map[string]Vendor
}

func NewFimirrorSyncer(config FirmirrorConfig) *FimirrorSyncer {
	return &FimirrorSyncer{
		Config:  config,
		vendors: make(map[string]Vendor),
	}
}

// PreflightCheck verifies that required external tools are available and creates necessary directories
func (f *FimirrorSyncer) PreflightCheck() error {
	// Check for required binaries
	requiredBinaries := []string{"fwupdtool"}
	for _, binary := range requiredBinaries {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf("required binary '%s' not found in PATH: %w", binary, err)
		}
		slog.Debug("Found required binary", "binary", binary)
	}

	// Create output directory if it doesn't exist
	requiredDirs := []string{f.Config.CacheDir, f.Config.OutputDir}
	for _, dir := range requiredDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
		slog.Debug("Directory ready", "path", f.Config.OutputDir)
	}

	return nil
}

// RegisterVendor registers a vendor with the given name
func (f *FimirrorSyncer) RegisterVendor(name string, vendor Vendor) {
	f.vendors[name] = vendor
}

// GetAllVendors returns all registered vendors
func (f *FimirrorSyncer) GetAllVendors() map[string]Vendor {
	// Return a copy to prevent external modifications
	return maps.Clone(f.vendors)
}

// processVendor processes firmware for a given vendor using the interface
func (f *FimirrorSyncer) ProcessVendor(vendor Vendor, vendorName string) error {
	logger := slog.With("vendor", vendorName)
	logger.Info("Fetching catalog")

	catalog, err := vendor.FetchCatalog()
	if err != nil {
		logger.Error("Failed to fetch catalog", "error", err)
		return err
	}

	entries := catalog.ListEntries()
	processed := 0

	for _, entry := range entries {
		fwName := entry.GetFilename()
		entryLogger := logger.With("firmware", fwName)
		entryLogger.Info("Processing firmware")

		appstream, workDir, err := vendor.ProcessFirmware(entry)
		if err != nil {
			entryLogger.Error("Failed to process firmware", "error", err)
			continue
		}

		if err = f.buildPackage(appstream, workDir); err != nil {
			entryLogger.Error("Failed to build package", "error", err)
			continue
		}

		processed++
		entryLogger.Info("Successfully processed firmware")
	}

	logger.Info("Completed vendor processing", "processed", processed)
	return nil
}

func (f *FimirrorSyncer) buildPackage(appstream *lvfs.Component, tmpDir string) error {
	outBytes := []byte(xml.Header)
	xmlBytes, err := xml.MarshalIndent(appstream, "", "  ")
	if err != nil {
		return err
	}
	outBytes = append(outBytes, xmlBytes...)

	fwFiles, err := os.ReadDir(tmpDir)
	if err != nil {
		return err
	}

	cabFile := fwFiles[0].Name() + ".cab"
	if _, err := os.Stat(path.Join(f.Config.OutputDir, cabFile)); err == nil {
		slog.Debug("Skipping cab build: file exists")
		return nil
	}

	fwupdArgs := []string{"build-cabinet", cabFile, path.Join(tmpDir, "/firmware.metainfo.xml")}
	for _, f := range fwFiles {
		fwupdArgs = append(fwupdArgs, path.Join(tmpDir, f.Name()))
	}

	err = os.WriteFile(path.Join(tmpDir, "/firmware.metainfo.xml"), outBytes, 0644)
	if err != nil {
		return err
	}

	cmd := exec.Command("fwupdtool", fwupdArgs...)
	cmd.Dir = f.Config.OutputDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("Failed to build package", "error", err, "output", string(out))
		return err
	}

	return nil
}
