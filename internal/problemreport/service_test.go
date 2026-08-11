package problemreport

import "testing"

func TestSanitizeContextRemovesRoomAndInviteSecrets(t *testing.T) {
	invite := sanitizeContext(TechnicalContext{Route: "/i/secret-invite-token?source=telegram", AppBuild: " preview ", RequestID: " request-1 "})
	if invite.Route != "/i/:invite" || invite.AppBuild != "preview" || invite.RequestID != "request-1" {
		t.Fatalf("unexpected invite context: %#v", invite)
	}
	room := sanitizeContext(TechnicalContext{Route: "/r/ABC234/recap?media=secret"})
	if room.Route != "/r/:room/recap" {
		t.Fatalf("room route was not sanitized: %q", room.Route)
	}
}

func TestValidateProblemReportValues(t *testing.T) {
	for _, category := range []string{"upload", "download", "room", "telegram", "other"} {
		if !validCategory(category) {
			t.Fatalf("valid category %q was rejected", category)
		}
	}
	if validCategory("security") || validStatus("deleted") {
		t.Fatal("unknown report values were accepted")
	}
}
