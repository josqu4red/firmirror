package firmirror

import "github.com/criteo/firmirror/pkg/lvfs"

type Vendor interface {
	// FetchCatalog retrieves the catalog of firmware for the vendor.
	FetchCatalog() (Catalog, error)
	// ProcessFirmware processes the firmware entry, downloading to cache if necessary
	// and converting to AppStream format. Returns the AppStream component and the
	// working directory path containing firmware files, or an error.
	ProcessFirmware(entry FirmwareEntry) (*lvfs.Component, string, error)
}

// Catalog represents a generic catalog of firmware entries.
type Catalog interface {
	// ListEntries returns all firmware entries in the catalog.
	ListEntries() []FirmwareEntry
}

// FirmwareEntry represents a single firmware entry in the catalog.
type FirmwareEntry interface {
	// GetFilename will be used to determine if the firmware has already been downloaded
	// and if it should be processed
	GetFilename() string
}
