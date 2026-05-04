// ABOUTME: Tests for version package symbols
// ABOUTME: Ensures version information is properly defined and ldflags-patchable
package version

import (
	"testing"
)

func TestVersionDefined(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestProductDefined(t *testing.T) {
	if Product == "" {
		t.Error("Product should not be empty")
	}
}

func TestManufacturerDefined(t *testing.T) {
	if Manufacturer == "" {
		t.Error("Manufacturer should not be empty")
	}
}

func TestVersionFormat(t *testing.T) {
	// Version should typically be in format like "0.1.0" or "dev"
	if len(Version) == 0 {
		t.Error("Version string is empty")
	}

	// Just verify it's a reasonable string
	if len(Version) > 100 {
		t.Error("Version string is unreasonably long")
	}
}

func TestProductFormat(t *testing.T) {
	// Product name should be reasonable length
	if len(Product) == 0 {
		t.Error("Product name is empty")
	}

	if len(Product) > 100 {
		t.Error("Product name is unreasonably long")
	}
}

func TestManufacturerFormat(t *testing.T) {
	// Manufacturer should be reasonable
	if len(Manufacturer) == 0 {
		t.Error("Manufacturer is empty")
	}

	if len(Manufacturer) > 100 {
		t.Error("Manufacturer name is unreasonably long")
	}
}

func TestVersionLdflagsPatchable(t *testing.T) {
	// Version, Product, and Manufacturer are package-level string vars so the
	// release workflow can patch Version via -ldflags "-X .../version.Version=v1.6.3".
	// This test just verifies the symbols are accessible. We deliberately don't
	// assert immutability — that's exactly what we gave up to make ldflags work.
	if Version == "" {
		t.Error("Version is empty")
	}
	if Product == "" {
		t.Error("Product is empty")
	}
	if Manufacturer == "" {
		t.Error("Manufacturer is empty")
	}
}

func TestVersionNotPlaceholder(t *testing.T) {
	// Check for common placeholder values
	placeholders := []string{"TODO", "FIXME", "XXX", "placeholder"}

	for _, placeholder := range placeholders {
		if Version == placeholder {
			t.Errorf("Version should not be placeholder value: %s", placeholder)
		}
		if Product == placeholder {
			t.Errorf("Product should not be placeholder value: %s", placeholder)
		}
		if Manufacturer == placeholder {
			t.Errorf("Manufacturer should not be placeholder value: %s", placeholder)
		}
	}
}
