package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// Storage keys under a multi-tenant application (OR-79).
//
// The store a multi-tenant application hands the panel prefixes every key
// with the request's tenant (storage.TenantStore): the panel writes
// "_tmp/import_…" and the object lands at "t1/_tmp/import_…". The ObjectInfo
// it answers names that PHYSICAL key, and the panel used to hand it to the
// client — the import upload, the export, the fixture dump — so the next
// step, sending it back to the same store, prefixed it a second time and
// read "t1/t1/_tmp/…", which does not exist. Import, loaddata and the export
// download did not work in a multi-tenant application.
//
// The panel now hands out the LOGICAL key, the one it asked the store to
// write, and takes logical keys back: the store adds the tenant once, the
// way it does for every key the application itself uses. The physical key
// never leaves the server.
//
// A client that still holds a physical key from an earlier version is
// served too: a key that starts with THIS request's tenant prefix and names
// a panel key under it ("t1/_tmp/…") is read back as the logical key it
// stands for. A key prefixed with another tenant is not stripped, so the
// store prefixes it with this request's tenant and finds nothing — the
// store, not this function, is what keeps a tenant inside its key space,
// and stripping only the request's own prefix cannot widen it.

// storageTenantPrefix is the prefix the application's store puts before
// every key of request r: the tenant the host resolved for it, which is
// what the store reads from the request scope — not the tenant the panel
// confines Data Studio to, which a superuser may switch with ?tenant= while
// the store keeps the host's. "" when the host resolved none.
func (p *Panel) storageTenantPrefix(r *http.Request) string {
	if p.config.TenantResolver == nil || r == nil {
		return ""
	}
	tenant, ok := p.config.TenantResolver(r)
	tenant = strings.TrimRight(strings.TrimSpace(tenant), "/")
	if !ok || tenant == "" {
		return ""
	}
	return tenant + "/"
}

// logicalStorageKey reads a key a client sends back to the panel as the
// logical key the store takes: as it came, or without this request's own
// tenant prefix when it is the physical key of a panel object (one under
// _tmp/) that an earlier version handed out.
func (p *Panel) logicalStorageKey(r *http.Request, key string) string {
	key = strings.TrimSpace(key)
	prefix := p.storageTenantPrefix(r)
	if prefix == "" || !strings.HasPrefix(key, prefix) {
		return key
	}
	if rest := strings.TrimPrefix(key, prefix); storage.IsTempKey(rest) {
		return rest
	}
	return key
}

// storageNotFound reports whether err is the store's "no such object".
func storageNotFound(err error) bool {
	var notFound storage.ErrNotFound
	return errors.As(err, &notFound)
}
