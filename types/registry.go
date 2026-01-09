package types

import (
	"maps"
)

// VendorRegistry manages all available vendors
type VendorRegistry struct {
	vendors map[string]Vendor
}

// NewVendorRegistry creates a new vendor registry
func NewVendorRegistry() *VendorRegistry {
	return &VendorRegistry{
		vendors: make(map[string]Vendor),
	}
}

// RegisterVendor registers a vendor with the given name
func (vr *VendorRegistry) RegisterVendor(name string, vendor Vendor) {
	vr.vendors[name] = vendor
}

// GetAllVendors returns all registered vendors
func (vr *VendorRegistry) GetAllVendors() map[string]Vendor {
	// Return a copy to prevent external modifications
	return maps.Clone(vr.vendors)
}

