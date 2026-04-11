// Package destruction — HTTP handlers for destruction reports.
package destruction

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// Service wraps the Generator plus a Postgres-backed list/get.
type Service struct {
	gen *Generator
}

// NewService creates the destruction service.
func NewService(gen *Generator) *Service {
	return &Service{gen: gen}
}

// ListHandler — GET /v1/destruction-reports
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		rows, err := svc.gen.pg.Query(r.Context(),
			`SELECT id, tenant_id::text, period, period_start, period_end, generated_at, minio_path, signing_key_id
			 FROM destruction_reports WHERE tenant_id = $1::uuid ORDER BY generated_at DESC`,
			p.TenantID,
		)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		defer rows.Close()

		var items []map[string]any
		for rows.Next() {
			var r Report
			if err := rows.Scan(&r.ID, &r.TenantID, &r.Period, &r.PeriodStart, &r.PeriodEnd, &r.GeneratedAt, &r.MinIOPath, &r.SigningKeyID); err != nil {
				break
			}
			items = append(items, map[string]any{
				"id": r.ID, "period": r.Period,
				"period_start": r.PeriodStart, "period_end": r.PeriodEnd,
				"generated_at": r.GeneratedAt,
			})
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// GetHandler — GET /v1/destruction-reports/{reportID}
func GetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "reportID")
		row := svc.gen.pg.QueryRow(r.Context(),
			`SELECT id, tenant_id::text, period, period_start, period_end, generated_at, minio_path, manifest, signing_key_id, signature
			 FROM destruction_reports WHERE id = $1 AND tenant_id = $2::uuid`,
			id, p.TenantID,
		)
		var rep Report
		err := row.Scan(&rep.ID, &rep.TenantID, &rep.Period, &rep.PeriodStart, &rep.PeriodEnd,
			&rep.GeneratedAt, &rep.MinIOPath, &rep.Manifest, &rep.SigningKeyID, &rep.Signature)
		if err != nil {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, rep)
	}
}

// DownloadHandler — GET /v1/destruction-reports/{reportID}/download (DPO only)
func DownloadHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if !auth.HasRole(p, auth.RoleDPO) {
			httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
			return
		}

		id := chi.URLParam(r, "reportID")

		// Audit the download.
		rec := audit.FromContext(r.Context())
		_, err := rec.Append(r.Context(), audit.Entry{
			Actor:    p.UserID,
			TenantID: p.TenantID,
			Action:   audit.ActionExportDownloaded,
			Target:   "destruction_report:" + id,
			Details:  map[string]any{"report_id": id},
		})
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}

		// Issue a short-lived presigned URL via MinIO.
		var minioPath string
		_ = svc.gen.pg.QueryRow(r.Context(),
			`SELECT minio_path FROM destruction_reports WHERE id = $1 AND tenant_id = $2::uuid`,
			id, p.TenantID,
		).Scan(&minioPath)

		if minioPath == "" {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}

		url, err := svc.gen.minioClient.PresignedGetURL(r.Context(), minioPath, 60*time.Second)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"url": url, "expires_in": 60})
	}
}

// GenerateHandler — POST /v1/destruction-reports/generate (manual trigger)
func GenerateHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())

		now := time.Now().UTC()
		periodStart, periodEnd := PeriodBounds(now)

		rep, err := svc.gen.Generate(r.Context(), p.TenantID, periodStart, periodEnd)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, rep)
	}
}
