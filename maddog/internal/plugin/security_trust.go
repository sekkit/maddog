package plugin

import (
	"path/filepath"

	"maddog/internal/mcpcatalog"
	"maddog/internal/mcptrust"
)

// ApplyProductionTrust attaches Maddog's compiled catalog trust root and the
// user's local capability receipts. A release without a compiled key fails
// closed only for official/catalog-required or strict-reader servers; ordinary
// custom MCP servers remain usable under the normal permission policy.
func ApplyProductionTrust(s Spec, stateHome, cacheDir string) Spec {
	if s.CatalogAuthority == nil {
		s.CatalogAuthority = mcpcatalog.Authority{
			Store:     mcpcatalog.NewFileStore(filepath.Join(cacheDir, "mcp-catalog", "catalog.json")),
			PublicKey: mcpcatalog.ProductionPublicKey(),
		}
	}
	if s.TrustAuthority == nil {
		s.TrustAuthority = mcptrust.ReceiptAuthority{
			Store: mcptrust.NewFileStore(filepath.Join(stateHome, "mcp-trust", "receipts.json")),
		}
	}
	return s
}
