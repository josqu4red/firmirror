package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/criteo/firmirror/pkg/firmirror"
	"github.com/criteo/firmirror/pkg/vendors/dell"
	"github.com/criteo/firmirror/pkg/vendors/hpe"
)

func main() {
	cliCtx := kong.Parse(&firmirror.CLI)
	switch cliCtx.Command() {
	case "refresh <out-dir>":
	default:
		panic(cliCtx.Command())
	}

	// Setup signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmConf := firmirror.FirmirrorConfig{
		OutputDir: firmirror.CLI.Refresh.OutDir,
		CacheDir:  firmirror.CLI.Refresh.CacheDir,
	}

	if !firmirror.CLI.HPEFlags.Enable && !firmirror.CLI.DellFlags.Enable {
		slog.Error("No vendor enabled, exiting")
		return
	}

	fm := firmirror.NewFimirrorSyncer(fmConf)

	// Run preflight checks
	if err := fm.PreflightCheck(); err != nil {
		slog.Error("Preflight check failed", "error", err)
		os.Exit(1)
	}

	if firmirror.CLI.HPEFlags.Enable {
		for _, gen := range firmirror.CLI.HPEFlags.Gens {
			hpeRepo := "fwpp-" + gen
			hpeVendor := hpe.NewHPEVendor(fmConf.CacheDir, hpeRepo)
			fm.RegisterVendor("hpe-"+gen, hpeVendor)
		}
	}

	if firmirror.CLI.DellFlags.Enable {
		dellVendor := dell.NewDellVendor(fmConf.CacheDir, firmirror.CLI.DellFlags.MachinesID)
		fm.RegisterVendor("dell", dellVendor)
	}

	slog.Info("Starting firmware processing", "vendors", len(fm.GetAllVendors()))

	for vendorName, vendor := range fm.GetAllVendors() {
		// Check if shutdown requested before starting new vendor
		if ctx.Err() != nil {
			slog.Info("Shutdown requested, stopping processing")
			break
		}

		slog.Info("Processing vendor", "name", vendorName)
		if err := fm.ProcessVendor(ctx, vendor, vendorName); err != nil {
			// Don't log context cancellation as an error
			if err == context.Canceled {
				slog.Info("Vendor processing cancelled", "vendor", vendorName)
			} else {
				slog.Error("Failed to process vendor", "vendor", vendorName, "error", err)
			}
		}
	}

	slog.Info("Firmware processing completed")
}
