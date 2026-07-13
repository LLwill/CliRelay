package identity

import "testing"

func TestNormalizeAuditPage(t *testing.T) {
	page, size := normalizeAuditPage(0, 0)
	if page != 1 || size != defaultAuditLogPageSize {
		t.Fatalf("defaults page=%d size=%d", page, size)
	}
	page, size = normalizeAuditPage(-3, 500)
	if page != 1 || size != maxAuditLogPageSize {
		t.Fatalf("clamped page=%d size=%d", page, size)
	}
	page, size = normalizeAuditPage(2, 20)
	if page != 2 || size != 20 {
		t.Fatalf("passthrough page=%d size=%d", page, size)
	}
}
