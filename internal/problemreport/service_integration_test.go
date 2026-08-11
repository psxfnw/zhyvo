package problemreport_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/problemreport"
)

func TestProblemReportLifecycleAndAdminAuthorization(t *testing.T) {
	databaseURL := os.Getenv("PHOTODROP_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PHOTODROP_INTEGRATION_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}

	adminTelegramID := int64(9_001_337)
	adminIdentityID, reporterIdentityID := uuid.New(), uuid.New()
	if _, err := db.Exec(ctx, `INSERT INTO identities (id, kind, display_name, telegram_user_id) VALUES ($1, 'telegram', 'Report admin', $2), ($3, 'anonymous', 'Reporter', NULL)`, adminIdentityID, adminTelegramID, reporterIdentityID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM problem_reports WHERE reporter_identity_id = $1`, reporterIdentityID)
		_, _ = db.Exec(ctx, `DELETE FROM identities WHERE id = ANY($1)`, []uuid.UUID{adminIdentityID, reporterIdentityID})
		db.Close()
	})
	service := problemreport.New(db, []int64{adminTelegramID})
	report, err := service.Create(ctx, &reporterIdentityID, problemreport.CreateInput{
		Category: "upload", Description: "Відео зупиняється після вибору", Contact: "@reporter",
		TechnicalContext: problemreport.TechnicalContext{Route: "/i/private-token?secret=yes", ErrorCode: "UPLOAD_FAILED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TechnicalContext.Route != "/i/:invite" {
		t.Fatalf("private route was stored: %q", report.TechnicalContext.Route)
	}
	allowed, err := service.IsAdmin(ctx, adminIdentityID)
	if err != nil || !allowed {
		t.Fatalf("configured Telegram admin was rejected: %v", err)
	}
	allowed, err = service.IsAdmin(ctx, reporterIdentityID)
	if err != nil || allowed {
		t.Fatalf("anonymous reporter received admin access: %v", err)
	}
	listed, err := service.List(ctx, "new", "upload")
	if err != nil || len(listed) == 0 {
		t.Fatalf("report was not listed: %v", err)
	}
	updated, err := service.Update(ctx, report.ID, "in_progress", "Перевіряємо відновлення upload")
	if err != nil || updated.Status != "in_progress" || updated.AdminNote == nil {
		t.Fatalf("report was not updated: %#v, %v", updated, err)
	}
	var pending int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM admin_notification_outbox WHERE problem_report_id = $1 AND telegram_user_id = $2`, report.ID, adminTelegramID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("admin notification was not enqueued: %d, %v", pending, err)
	}
	stats, err := service.Stats(ctx)
	if err != nil || stats.TotalUsers < 2 {
		t.Fatalf("stats unavailable: %#v, %v", stats, err)
	}
}
