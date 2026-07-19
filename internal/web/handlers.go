package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aberti/cnpg-portal/internal/cnpg"
	"github.com/aberti/cnpg-portal/internal/pg"
	"github.com/aberti/cnpg-portal/internal/tenant"
	"github.com/aberti/cnpg-portal/internal/web/templates"
)

// Handlers groups every HTTP handler against shared deps + logger so the
// chi router stays a thin wiring layer.
type Handlers struct {
	Deps        tenant.Deps
	Logger      *slog.Logger
	Auth        Auth
	ClusterID   string
	DisplayName string
	BasePath    string
}

func (h *Handlers) route(path string) string {
	return strings.TrimSuffix(h.BasePath, "/") + "/" + strings.TrimPrefix(path, "/")
}

// NewTenantForm serves GET /new — admin-only page with tenant creation form.
func (h *Handlers) NewTenantForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.NewTenantForm("", "").Render(r.Context(), w); err != nil {
		h.Logger.Error("render new tenant form", "err", err)
	}
}

// NewTenantSubmit serves POST /new — provisions the tenant and renders the
// one-time credentials result.
func (h *Handlers) NewTenantSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "invalid form", err.Error())
		return
	}
	app := strings.TrimSpace(r.FormValue("app"))
	if !pg.IdentSafe(app) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		if err := templates.NewTenantForm(app, "Tenant names must match [a-z][a-z0-9_]{0,62}.").Render(r.Context(), w); err != nil {
			h.Logger.Error("render new tenant form with error", "err", err)
		}
		return
	}
	if h.Deps.K8s == nil || h.Deps.PG == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"dependencies not configured",
			"cnpgctl serve was started without K8s/PG/workspace deps; check --workspace and --kubeconfig.")
		return
	}

	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit",
		"event", "tenant.create",
		"user", id.Login,
		"tenant", app,
		"rid", RequestID(r.Context()),
	)

	t, err := tenant.Provision(r.Context(), h.Deps, app)
	if err != nil {
		h.Logger.Error("provision tenant", "err", err, "tenant", app)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to create tenant", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.NewTenantCreated(t).Render(r.Context(), w); err != nil {
		h.Logger.Error("render tenant created page", "err", err)
	}
}

// maxImportUploadBytes caps multipart dump uploads (see ImportDumpSubmit).
const maxImportUploadBytes = 256 << 20 // 256 MiB

// ImportDumpForm serves GET /new/import-dump.
func (h *Handlers) ImportDumpForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ImportDumpForm("", "").Render(r.Context(), w); err != nil {
		h.Logger.Error("render import dump form", "err", err)
	}
}

// ImportDumpSubmit serves POST /new/import-dump — multipart upload then tenant.ImportFromDump.
func (h *Handlers) ImportDumpSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportUploadBytes+1)
	if err := r.ParseMultipartForm(maxImportUploadBytes); err != nil {
		h.importDumpFormError(w, r, "", "Could not read upload (too large or invalid multipart). Max size is 256 MiB; increase --read-timeout for slow connections.")
		return
	}
	app := strings.TrimSpace(r.FormValue("app"))
	if !pg.IdentSafe(app) {
		h.importDumpFormError(w, r, app, "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if h.Deps.K8s == nil || h.Deps.PG == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"dependencies not configured",
			"cnpgctl serve was started without K8s/PG/workspace deps; check --workspace and --kubeconfig.")
		return
	}
	file, _, err := r.FormFile("dump")
	if err != nil {
		h.importDumpFormError(w, r, app, "Missing dump file field \"dump\".")
		return
	}
	defer func() { _ = file.Close() }()
	var magic [5]byte
	if _, err := io.ReadFull(file, magic[:]); err != nil || string(magic[:]) != "PGDMP" {
		h.importDumpFormError(w, r, app, "Only PostgreSQL custom-format dumps created with pg_dump -Fc are accepted. Plain SQL imports are disabled in the web UI.")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.importDumpFormError(w, r, app, "Could not rewind the uploaded dump.")
		return
	}

	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit",
		"event", "tenant.import_dump",
		"user", id.Login,
		"tenant", app,
		"rid", RequestID(r.Context()),
	)

	t, err := tenant.ImportFromDump(r.Context(), h.Deps, app, file)
	if err != nil {
		h.Logger.Error("import dump", "err", err, "tenant", app)
		h.renderError(w, r, http.StatusInternalServerError, "Import failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.NewTenantCreated(t).Render(r.Context(), w); err != nil {
		h.Logger.Error("render tenant created page", "err", err)
	}
}

func (h *Handlers) importDumpFormError(w http.ResponseWriter, r *http.Request, prefill, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	if err := templates.ImportDumpForm(prefill, msg).Render(r.Context(), w); err != nil {
		h.Logger.Error("render import dump form with error", "err", err)
	}
}

// BranchTenantForm serves GET /tenant/{name}/branch — admin-only page.
func (h *Handlers) BranchTenantForm(w http.ResponseWriter, r *http.Request) {
	src := chi.URLParam(r, "name")
	if !pg.IdentSafe(src) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	def := tenant.DefaultBranchDestination(src)
	if err := templates.BranchTenantForm(src, def, "").Render(r.Context(), w); err != nil {
		h.Logger.Error("render branch form", "err", err)
	}
}

// BranchTenantSubmit serves POST /tenant/{name}/branch.
func (h *Handlers) BranchTenantSubmit(w http.ResponseWriter, r *http.Request) {
	src := chi.URLParam(r, "name")
	if !pg.IdentSafe(src) {
		h.renderError(w, r, http.StatusBadRequest, "invalid source tenant", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "invalid form", err.Error())
		return
	}
	dst := strings.ToLower(strings.TrimSpace(r.FormValue("dst")))
	def := tenant.DefaultBranchDestination(src)
	var errMsg string
	switch {
	case dst == src:
		errMsg = "Destination must be different from the source tenant."
	case !pg.IdentSafe(dst):
		errMsg = "Use a lowercase name: start with a letter, then letters, digits, or underscore only (max 63 characters). Example: " + def + "."
	default:
		errMsg = ""
	}
	if errMsg != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		if err := templates.BranchTenantForm(src, def, errMsg).Render(r.Context(), w); err != nil {
			h.Logger.Error("render branch form with error", "err", err)
		}
		return
	}
	if h.Deps.K8s == nil || h.Deps.PG == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"dependencies not configured",
			"cnpgctl serve was started without K8s/PG/workspace deps; check --workspace and --kubeconfig.")
		return
	}
	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit", "event", "tenant.branch", "user", id.Login, "src", src, "dst", dst, "rid", RequestID(r.Context()))
	t, err := tenant.Branch(r.Context(), h.Deps, src, dst)
	if err != nil {
		h.Logger.Error("branch tenant", "err", err, "src", src, "dst", dst)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to branch tenant", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.BranchTenantCreated(src, t).Render(r.Context(), w); err != nil {
		h.Logger.Error("render branch success", "err", err)
	}
}

// SyncTenantForm serves GET /tenant/{name}/sync — admin-only. {name} is the
// *destination* tenant; the form picks a source from the dropdown of all
// other tenants and overwrites this tenant's database with the source.
func (h *Handlers) SyncTenantForm(w http.ResponseWriter, r *http.Request) {
	dst := chi.URLParam(r, "name")
	if !pg.IdentSafe(dst) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if h.Deps.PG == nil {
		h.renderError(w, r, http.StatusServiceUnavailable, "database client not configured", "cnpgctl serve was started without Postgres deps.")
		return
	}
	sources, err := h.listSyncSources(r.Context(), dst)
	if err != nil {
		h.Logger.Error("list tenants for sync", "err", err)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to list source tenants", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.SyncTenantForm(dst, sources, "").Render(r.Context(), w); err != nil {
		h.Logger.Error("render sync form", "err", err)
	}
}

// SyncTenantSubmit serves POST /tenant/{name}/sync.
func (h *Handlers) SyncTenantSubmit(w http.ResponseWriter, r *http.Request) {
	dst := chi.URLParam(r, "name")
	if !pg.IdentSafe(dst) {
		h.renderError(w, r, http.StatusBadRequest, "invalid destination tenant", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "invalid form", err.Error())
		return
	}
	src := strings.ToLower(strings.TrimSpace(r.FormValue("src")))
	confirm := strings.TrimSpace(r.FormValue("confirm"))

	var errMsg string
	switch {
	case !pg.IdentSafe(src):
		errMsg = "Choose a source tenant from the list."
	case src == dst:
		errMsg = "Source must be different from the destination tenant."
	case confirm != dst:
		errMsg = "Confirmation must exactly match the destination tenant name."
	}
	if errMsg != "" {
		sources, lerr := h.listSyncSources(r.Context(), dst)
		if lerr != nil {
			h.renderError(w, r, http.StatusInternalServerError, "Failed to list source tenants", lerr.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		if err := templates.SyncTenantForm(dst, sources, errMsg).Render(r.Context(), w); err != nil {
			h.Logger.Error("render sync form with error", "err", err)
		}
		return
	}
	if h.Deps.K8s == nil || h.Deps.PG == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"dependencies not configured",
			"cnpgctl serve was started without K8s/PG/workspace deps; check --workspace and --kubeconfig.")
		return
	}
	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit", "event", "tenant.sync", "user", id.Login, "src", src, "dst", dst, "rid", RequestID(r.Context()))
	if err := tenant.Sync(r.Context(), h.Deps, src, dst, tenant.SyncOptions{Confirmed: true}); err != nil {
		h.Logger.Error("sync tenant", "err", err, "src", src, "dst", dst)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to sync tenant", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.SyncTenantDone(src, dst).Render(r.Context(), w); err != nil {
		h.Logger.Error("render sync success", "err", err)
	}
}

// listSyncSources returns the sorted names of all tenants except `excluded`,
// ready to render in the sync source dropdown.
func (h *Handlers) listSyncSources(ctx context.Context, excluded string) ([]string, error) {
	tenants, err := tenant.List(ctx, h.Deps)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tenants))
	for i := range tenants {
		if tenants[i].Name == excluded {
			continue
		}
		names = append(names, tenants[i].Name)
	}
	return names, nil
}

// DropTenantForm serves GET /tenant/{name}/drop — admin-only page.
func (h *Handlers) DropTenantForm(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "name")
	if !pg.IdentSafe(app) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.DropTenantForm(app, "").Render(r.Context(), w); err != nil {
		h.Logger.Error("render drop form", "err", err)
	}
}

// DropTenantSubmit serves POST /tenant/{name}/drop.
func (h *Handlers) DropTenantSubmit(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "name")
	if !pg.IdentSafe(app) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "invalid form", err.Error())
		return
	}
	confirm := strings.TrimSpace(r.FormValue("confirm"))
	if confirm != app {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		if err := templates.DropTenantForm(app, "Confirmation must exactly match tenant name.").Render(r.Context(), w); err != nil {
			h.Logger.Error("render drop form with error", "err", err)
		}
		return
	}
	if h.Deps.K8s == nil || h.Deps.PG == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"dependencies not configured",
			"cnpgctl serve was started without K8s/PG/workspace deps; check --workspace and --kubeconfig.")
		return
	}
	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit", "event", "tenant.drop", "user", id.Login, "tenant", app, "rid", RequestID(r.Context()))
	if err := tenant.Drop(r.Context(), h.Deps, app, tenant.DropOptions{Confirmed: true}); err != nil {
		h.Logger.Error("drop tenant", "err", err, "tenant", app)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to drop tenant", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.DropTenantDone(app).Render(r.Context(), w); err != nil {
		h.Logger.Error("render drop success", "err", err)
	}
}

// restoreTemporarilyDisabled was a kill switch flipped on 2026-04-30 after
// the F4 restore verb hung on the wait-for-recovery-pod step. Root cause
// was PrimaryPodName matching the CNPG bootstrap-job pod (which has no
// `postgres` container) instead of the actual instance pod; cnpg-portal
// now filters by cnpg.io/podRole=instance. RecoveryClusterName also
// gained underscore→hyphen normalization. End-to-end run on tenant "app"
// completed in ~95s; gate kept as a const for fast re-disable if needed.
const restoreTemporarilyDisabled = false

// RotateTenantForm serves GET /tenant/{name}/rotate — admin-only.
func (h *Handlers) RotateTenantForm(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "name")
	if !pg.IdentSafe(app) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.RotateTenantForm(app, "").Render(r.Context(), w); err != nil {
		h.Logger.Error("render rotate form", "err", err)
	}
}

// RotateTenantSubmit serves POST /tenant/{name}/rotate.
func (h *Handlers) RotateTenantSubmit(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "name")
	if !pg.IdentSafe(app) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "invalid form", err.Error())
		return
	}
	confirm := strings.TrimSpace(r.FormValue("confirm"))
	if confirm != app {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		if err := templates.RotateTenantForm(app, "Confirmation must exactly match tenant name.").Render(r.Context(), w); err != nil {
			h.Logger.Error("render rotate form with error", "err", err)
		}
		return
	}
	if h.Deps.K8s == nil || h.Deps.PG == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"dependencies not configured",
			"cnpgctl serve was started without K8s/PG/workspace deps; check --workspace and --kubeconfig.")
		return
	}
	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit", "event", "tenant.rotate", "user", id.Login, "tenant", app, "rid", RequestID(r.Context()))
	t, err := tenant.Rotate(r.Context(), h.Deps, app, tenant.RotateOptions{Confirmed: true})
	if err != nil {
		h.Logger.Error("rotate tenant", "err", err, "tenant", app)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to rotate credentials", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.RotateTenantDone(t).Render(r.Context(), w); err != nil {
		h.Logger.Error("render rotate success", "err", err)
	}
}

// RestoreTenantForm serves GET /tenant/{name}/restore — admin-only.
func (h *Handlers) RestoreTenantForm(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "name")
	if !pg.IdentSafe(app) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if restoreTemporarilyDisabled {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"Restore from backup is temporarily disabled",
			"Re-validation in progress after the 2026-04-30 hang/orphan-cluster incident. Use the CLI ('cnpgctl restore <app> --backup=<name>') only if you can supervise the run; stuck recovery clusters need manual `kubectl delete cluster -n pg pg-restore-<app>-<id>` cleanup.")
		return
	}
	if h.Deps.K8s == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"dependencies not configured",
			"cnpgctl serve was started without K8s/workspace deps; check --workspace and --kubeconfig.")
		return
	}
	backups, err := cnpg.ListBackups(r.Context(), h.Deps.K8s.Dynamic, h.Deps.Workspace.Namespace, h.Deps.Workspace.ClusterName)
	if err != nil {
		h.Logger.Error("list backups for restore form", "err", err)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to list backups", err.Error())
		return
	}
	choices := make([]templates.RestoreBackupChoice, 0, len(backups))
	for i := range backups {
		b := &backups[i]
		if b.Phase != cnpg.PhaseCompleted {
			continue
		}
		when := b.StoppedAt.UTC().Format(time.RFC3339)
		if b.StoppedAt.IsZero() {
			when = b.StartedAt.UTC().Format(time.RFC3339)
		}
		choices = append(choices, templates.RestoreBackupChoice{Name: b.Name, When: when})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.RestoreTenantForm(app, choices, "").Render(r.Context(), w); err != nil {
		h.Logger.Error("render restore form", "err", err)
	}
}

// RestoreTenantSubmit serves POST /tenant/{name}/restore — admin-only.
func (h *Handlers) RestoreTenantSubmit(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "name")
	if !pg.IdentSafe(app) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if restoreTemporarilyDisabled {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"Restore from backup is temporarily disabled",
			"Re-validation in progress after the 2026-04-30 hang/orphan-cluster incident. Use the CLI for now.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "invalid form", err.Error())
		return
	}
	confirm := strings.TrimSpace(r.FormValue("confirm"))
	if confirm != app {
		h.restoreFormWithError(w, r, app, "Confirmation must exactly match tenant name.")
		return
	}
	backupName := strings.TrimSpace(r.FormValue("backup"))
	if backupName == "" || !cnpg.BackupObjectNameOK(backupName) {
		h.restoreFormWithError(w, r, app, "Select a valid backup.")
		return
	}
	var targetTime *time.Time
	if ts := strings.TrimSpace(r.FormValue("target_time")); ts != "" {
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			h.restoreFormWithError(w, r, app, "target_time must be RFC3339 (e.g. 2006-01-02T15:04:05Z).")
			return
		}
		targetTime = &t
	}
	if h.Deps.K8s == nil || h.Deps.PG == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"dependencies not configured",
			"cnpgctl serve was started without full deps; check --workspace and --kubeconfig.")
		return
	}
	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit", "event", "tenant.restore", "user", id.Login, "tenant", app, "backup", backupName, "rid", RequestID(r.Context()))

	ctx, cancel := contextWithTimeout(r.Context(), 85*time.Minute)
	defer cancel()
	if err := tenant.RestoreFromBackup(ctx, h.Deps, app, backupName, targetTime); err != nil {
		h.Logger.Error("restore tenant", "err", err, "tenant", app)
		h.restoreFormWithError(w, r, app, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.RestoreTenantDone(app).Render(r.Context(), w); err != nil {
		h.Logger.Error("render restore done", "err", err)
	}
}

func (h *Handlers) restoreFormWithError(w http.ResponseWriter, r *http.Request, app, errMsg string) {
	if h.Deps.K8s == nil || h.Deps.Workspace == nil {
		h.renderError(w, r, http.StatusServiceUnavailable, "dependencies not configured", errMsg)
		return
	}
	backups, err := cnpg.ListBackups(r.Context(), h.Deps.K8s.Dynamic, h.Deps.Workspace.Namespace, h.Deps.Workspace.ClusterName)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Failed to list backups", err.Error())
		return
	}
	choices := make([]templates.RestoreBackupChoice, 0, len(backups))
	for i := range backups {
		b := &backups[i]
		if b.Phase != cnpg.PhaseCompleted {
			continue
		}
		when := b.StoppedAt.UTC().Format(time.RFC3339)
		if b.StoppedAt.IsZero() {
			when = b.StartedAt.UTC().Format(time.RFC3339)
		}
		choices = append(choices, templates.RestoreBackupChoice{Name: b.Name, When: when})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	if err := templates.RestoreTenantForm(app, choices, errMsg).Render(r.Context(), w); err != nil {
		h.Logger.Error("render restore form with error", "err", err)
	}
}

// TenantConnections serves GET /tenant/{name}/conns — read-only per-tenant
// pg_stat_activity view. Terminate buttons are rendered only for admins.
func (h *Handlers) TenantConnections(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !pg.IdentSafe(name) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if h.Deps.PG == nil {
		h.renderError(w, r, http.StatusServiceUnavailable, "database client not configured", "cnpgctl serve was started without Postgres deps.")
		return
	}
	rows, err := tenant.Connections(r.Context(), h.Deps, name)
	if err != nil {
		h.Logger.Error("tenant connections", "tenant", name, "err", err)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to load connections", err.Error())
		return
	}
	id, ok := IdentityFrom(r.Context())
	isAdmin := ok && h.Auth.Admins != nil && h.Auth.Admins.Has(id.Login)
	msg := r.URL.Query().Get("msg")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.TenantConnections(name, rows, isAdmin, msg).Render(r.Context(), w); err != nil {
		h.Logger.Error("render tenant connections", "err", err)
	}
}

// TerminateTenantConnection serves POST /tenant/{name}/conns/{pid}/terminate.
func (h *Handlers) TerminateTenantConnection(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !pg.IdentSafe(name) {
		h.renderError(w, r, http.StatusBadRequest, "invalid tenant name", "Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	pid, err := strconv.Atoi(chi.URLParam(r, "pid"))
	if err != nil || pid <= 0 {
		h.renderError(w, r, http.StatusBadRequest, "invalid pid", "PID must be a positive integer.")
		return
	}
	if h.Deps.PG == nil {
		h.renderError(w, r, http.StatusServiceUnavailable, "database client not configured", "cnpgctl serve was started without Postgres deps.")
		return
	}
	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit",
		"event", "connection.terminate",
		"user", id.Login,
		"tenant", name,
		"pid", pid,
		"rid", RequestID(r.Context()),
	)
	if err := tenant.TerminateConnection(r.Context(), h.Deps, name, pid); err != nil {
		h.Logger.Error("terminate connection", "tenant", name, "pid", pid, "err", err)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to terminate connection", err.Error())
		return
	}
	http.Redirect(w, r, h.route("/tenant/"+name+"/conns")+"?msg="+url.QueryEscape(fmt.Sprintf("terminated pid %d", pid)), http.StatusSeeOther)
}

// ListTenants serves GET / — the tenant inventory table.
func (h *Handlers) ListTenants(w http.ResponseWriter, r *http.Request) {
	if h.Deps.PG == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"database client not configured",
			"cnpgctl serve was started without Postgres deps; check the workspace marker and KUBECONFIG.")
		return
	}
	tenants, err := tenant.List(r.Context(), h.Deps)
	if err != nil {
		h.Logger.Error("list tenants", "err", err)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to list tenants", err.Error())
		return
	}
	id, ok := IdentityFrom(r.Context())
	if !ok || id.Login == "" {
		id.Login = "unknown"
	}
	isAdmin := h.Auth.Admins != nil && h.Auth.Admins.Has(id.Login)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.TenantList(tenants, id.Login, isAdmin).Render(r.Context(), w); err != nil {
		h.Logger.Error("render tenant list", "err", err)
	}
}

// TenantDetail serves GET /tenant/{name} — the focused per-tenant view.
// Validates {name} via pg.IdentSafe before any DB call so untrusted URL
// segments never reach SQL. Maps tenant.ErrTenantNotFound to a dedicated
// 404 page rather than a generic error frame.
func (h *Handlers) TenantDetail(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !pg.IdentSafe(name) {
		h.renderError(w, r, http.StatusBadRequest,
			"invalid tenant name",
			"Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if h.Deps.PG == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"database client not configured",
			"cnpgctl serve was started without Postgres deps; check the workspace marker and KUBECONFIG.")
		return
	}
	t, err := tenant.Status(r.Context(), h.Deps, name)
	if err != nil {
		if errors.Is(err, tenant.ErrTenantNotFound) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			if rerr := templates.TenantNotFound(name).Render(r.Context(), w); rerr != nil {
				h.Logger.Error("render tenant-not-found", "err", rerr)
			}
			return
		}
		h.Logger.Error("tenant status", "err", err, "tenant", name)
		h.renderError(w, r, http.StatusInternalServerError, "Failed to load tenant", err.Error())
		return
	}

	// Best-effort credentials read: Status drives the page, ReadCredentials
	// only enriches the "Connection strings" panel. A missing/locked Secret
	// degrades to a render-time placeholder rather than a 500 — the tenant
	// list is still useful when ESO is mid-sync or RBAC is incomplete.
	var creds *tenant.Credentials
	if t.Owner == t.Name && t.Login {
		var credsErr error
		creds, credsErr = tenant.ReadCredentials(r.Context(), h.Deps, name)
		if credsErr != nil && !errors.Is(credsErr, tenant.ErrCredentialsNotFound) {
			h.Logger.Warn("read credentials", "err", credsErr, "tenant", name)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	id, ok := IdentityFrom(r.Context())
	isAdmin := ok && h.Auth.Admins != nil && h.Auth.Admins.Has(id.Login)
	if err := templates.TenantDetail(t, creds, isAdmin).Render(r.Context(), w); err != nil {
		h.Logger.Error("render tenant detail", "err", err)
	}
}

// TriggerBackup serves POST /backup — creates an on-demand CNPG Backup CR
// and waits up to 5 min for it to reach a terminal phase. Returns the
// updated BackupStatus partial so htmx can swap the button area in place.
//
// Backups are cluster-wide (CNPG WAL + Barman base), so the verb lives at
// the page header level rather than per-tenant. The Backup CR is labelled
// `cnpg-portal/triggered-by=ui` for audit.
func (h *Handlers) TriggerBackup(w http.ResponseWriter, r *http.Request) {
	if h.Deps.K8s == nil || h.Deps.K8s.Dynamic == nil || h.Deps.Workspace == nil {
		h.renderBackupResult(w, r, nil, "K8s/workspace deps not configured")
		return
	}

	id, _ := IdentityFrom(r.Context())
	h.Logger.Info("audit",
		"event", "backup.trigger",
		"user", id.Login,
		"rid", RequestID(r.Context()),
	)

	ctx, cancel := contextWithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	b, err := cnpg.Trigger(ctx, h.Deps.K8s.Dynamic, h.Deps.Workspace.Namespace, h.Deps.Workspace.ClusterName, "ui")
	if err != nil {
		h.Logger.Error("trigger backup", "err", err)
		h.renderBackupResult(w, r, nil, err.Error())
		return
	}
	h.Logger.Info("backup triggered, waiting", "backup", b.Name)

	final, err := cnpg.Wait(ctx, h.Deps.K8s.Dynamic, h.Deps.Workspace.Namespace, b.Name)
	if err != nil {
		if errors.Is(err, cnpg.ErrBackupFailed) {
			h.renderBackupResult(w, r, final,
				fmt.Sprintf("backup %s failed (kubectl describe backup -n %s %s)",
					final.Name, h.Deps.Workspace.Namespace, final.Name))
			return
		}
		h.Logger.Error("wait backup", "err", err, "backup", b.Name)
		h.renderBackupResult(w, r, nil, err.Error())
		return
	}
	h.renderBackupResult(w, r, final, "")
}

// DumpTenant serves GET /tenant/{name}/dump — streams `pg_dump -Fc` of the
// tenant database directly to the response with Content-Disposition so
// browsers prompt to save. Custom format ("-Fc") is the same shape
// `cnpgctl dump -o file` produces, restorable with `pg_restore`.
func (h *Handlers) DumpTenant(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !pg.IdentSafe(name) {
		h.renderError(w, r, http.StatusBadRequest,
			"invalid tenant name",
			"Tenant names must match [a-z][a-z0-9_]{0,62}.")
		return
	}
	if h.Deps.PG == nil {
		h.renderError(w, r, http.StatusServiceUnavailable,
			"database client not configured",
			"cnpgctl serve was started without Postgres deps.")
		return
	}

	filename := fmt.Sprintf("%s-%s.dump", name, time.Now().UTC().Format("20060102T150405Z"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	if err := h.Deps.PG.PgDump(r.Context(), name, w); err != nil {
		// Headers were already sent — best we can do is log and stop.
		h.Logger.Error("pg_dump stream", "err", err, "tenant", name)
	}
}

// renderBackupResult writes the BackupStatus partial back through htmx.
func (h *Handlers) renderBackupResult(w http.ResponseWriter, r *http.Request, b *cnpg.Backup, errMsg string) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, h.route("/"), http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.BackupStatus(false, b, errMsg, true).Render(r.Context(), w); err != nil {
		h.Logger.Error("render backup status", "err", err)
	}
}

// renderError sends an HTML error page with the provided HTTP status.
// Used for handler-level failures (DB errors, missing deps).
func (h *Handlers) renderError(w http.ResponseWriter, r *http.Request, status int, title, body string) {
	if status >= http.StatusInternalServerError {
		body = "The operation failed. Check the server log with request ID " + RequestID(r.Context()) + "."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ErrorPage(title, body).Render(r.Context(), w); err != nil {
		h.Logger.Error("render error page", "err", err)
	}
}
