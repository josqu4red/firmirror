package main

import (
	"encoding/xml"
	"log/slog"
	"os"
	"os/exec"
	"path"

	"github.com/alecthomas/kong"

	"github.com/criteo/firmirror/cli"
	"github.com/criteo/firmirror/types"
	"github.com/criteo/firmirror/vendors/dell"
	"github.com/criteo/firmirror/vendors/hpe"
)

var outDir = ""

func buildPackage(tmpDir string, appstream *types.Component) error {
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

	fwupdArgs := []string{"build-cabinet", fwFiles[0].Name() + ".cab", path.Join(tmpDir, "/firmware.metainfo.xml")}
	for _, f := range fwFiles {
		fwupdArgs = append(fwupdArgs, path.Join(tmpDir, f.Name()))
	}

	err = os.WriteFile(path.Join(tmpDir, "/firmware.metainfo.xml"), outBytes, 0644)
	if err != nil {
		return err
	}

	cmd := exec.Command("fwupdtool", fwupdArgs...)
	cmd.Dir = outDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("Failed to build package", "error", err, "output", string(out))
		return err
	}

	return nil
}

// processVendor processes firmware for a given vendor using the interface
func processVendor(vendor types.Vendor, vendorName string, limit int) error {
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
		if processed >= limit {
			break
		}

		fwName := entry.GetFilename()
		entryLogger := logger.With("firmware", fwName)
		entryLogger.Info("Processing firmware")

		tmpDir, err := os.MkdirTemp(outDir, fwName+".wrk")
		if err != nil {
			entryLogger.Error("Failed to create temp directory", "error", err)
			continue
		}
		defer os.RemoveAll(tmpDir)

		// Retrieve firmware (downloads it for vendors that need it)
		_, err = vendor.RetrieveFirmware(entry, tmpDir)
		if err != nil {
			entryLogger.Error("Failed to retrieve firmware", "error", err)
			continue
		}

		// Convert to AppStream
		appstream, err := vendor.FirmwareToAppstream(entry)
		if err != nil {
			entryLogger.Error("Failed to convert firmware", "error", err)
			continue
		}

		// Build package
		err = buildPackage(tmpDir, appstream)
		if err != nil {
			entryLogger.Error("Failed to build package", "error", err)
			continue
		}

		processed++
		entryLogger.Info("Successfully processed firmware")
	}

	logger.Info("Completed vendor processing", "processed", processed)
	return nil
}

func main() {
	ctx := kong.Parse(&cli.CLI)
	switch ctx.Command() {
	case "refresh <out-dir>":
	default:
		panic(ctx.Command())
	}
	outDir = cli.CLI.Refresh.OutDir
	os.MkdirAll(outDir, 0o0755)

	if !cli.CLI.HPEFlags.Enable && !cli.CLI.DellFlags.Enable {
		slog.Error("No vendor enabled, exiting")
		return
	}

	// Create vendor registry
	registry := types.NewVendorRegistry()
	if cli.CLI.HPEFlags.Enable {
		for _, gen := range cli.CLI.HPEFlags.Gens {
			hpeRepo := "fwpp-" + gen
			hpeVendor := hpe.NewHPEVendor(hpeRepo)
			registry.RegisterVendor("hpe-"+gen, hpeVendor)
		}
	}

	if cli.CLI.DellFlags.Enable {
		dellVendor := dell.NewDellVendor(cli.CLI.DellFlags.MachinesID)
		registry.RegisterVendor("dell", dellVendor)
	}

	slog.Info("Starting firmware processing", "vendors", len(registry.GetAllVendors()))

	for vendorName, vendor := range registry.GetAllVendors() {
		slog.Info("Processing vendor", "name", vendorName)
		err := processVendor(vendor, vendorName, 10)
		if err != nil {
			slog.Error("Failed to process vendor", "vendor", vendorName, "error", err)
		}
	}

	slog.Info("Firmware processing completed")
}
