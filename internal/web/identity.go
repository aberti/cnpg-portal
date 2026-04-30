package web

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

// Tailscale identity headers, set by `tailscale serve` (or the Tailscale
// Operator) when it terminates the inbound tailnet connection. The pod
// receives plain HTTP from `tailscale serve`; trust comes from the
// network topology (only `tailscale serve` can route to the in-cluster
// Service NodePort)
const (
	HeaderLogin      = "Tailscale-User-Login"
	HeaderName       = "Tailscale-User-Name"
	HeaderProfilePic = "Tailscale-User-Profile-Pic"
)

// Identity is the per-request user record assembled from Tailscale
// headers. Login is the email; Name and PicURL are best-effort.
type Identity struct {
	Login  string
	Name   string
	PicURL string
}

// Auth bundles the per-request auth knobs Router needs. Zero value is
// fail-closed: empty DevLogin means real headers are required, nil Admins
// means no logins are admins (every mutation 403s).
type Auth struct {
	DevLogin string
	Admins   *AdminList

	// BearerToken + BearerIdentity enable Authorization: Bearer <token> without
	// Tailscale headers (Traefik, port-forward, etc.). Token compared in
	// constant time; identity is always treated as admin when bearer is configured.
	// Prefer mounting token from a Secret via --bearer-token-file in prod.
	BearerToken    string
	BearerIdentity string

	// TailnetFallbackLogin is optional. When non-empty and Tailscale-User-Login
	// is absent, the client IP (first X-Forwarded-For hop, else RemoteAddr) must
	// be in Tailscale CGNAT 100.64.0.0/10 — then this login is used. Use when
	// pgm is reachable only via tailnet DNS but Traefik does not inject
	// Tailscale headers (unlike tailscale serve). Everyone sharing that path
	// gets the same identity (fine for a single-operator tailnet).
	TailnetFallbackLogin string
}

const identityKey ctxKey = 2

// IdentityFrom returns the identity stored on ctx (after IdentityMiddleware).
// The second return is false when the middleware was bypassed — handlers
// that need an identity should treat that as a programming error.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(identityKey).(Identity)
	return v, ok
}

// IdentityMiddleware reads the Tailscale-User-* headers and attaches the
// resulting Identity to the request context. Requests without a login
// header are rejected with 403; that's the daily-use case where someone
// reaches the pod via a path that bypasses `tailscale serve`. devLogin,
// when non-empty, replaces the header read entirely — used by `cnpgctl
// serve --dev-login=…` for laptop development without the tailnet.
func IdentityMiddleware(auth Auth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth.BearerToken != "" && auth.BearerIdentity != "" {
				tok := parseBearerSecret(r)
				if tok != "" {
					if bearerTokensEqual(auth.BearerToken, tok) {
						ctx := context.WithValue(r.Context(), identityKey, Identity{
							Login: auth.BearerIdentity,
							Name:  "bearer (" + auth.BearerIdentity + ")",
						})
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
					http.Error(w, "invalid bearer token", http.StatusUnauthorized)
					return
				}
			}

			// Local-dev escape hatch — bypasses real header trust.
			if auth.DevLogin != "" {
				ctx := context.WithValue(r.Context(), identityKey,
					Identity{Login: auth.DevLogin, Name: "dev (" + auth.DevLogin + ")"})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			login := r.Header.Get(HeaderLogin)
			if login == "" && auth.TailnetFallbackLogin != "" {
				if ip := requestClientIP(r); IsTailscaleCGNAT(ip) {
					ctx := context.WithValue(r.Context(), identityKey, Identity{
						Login: auth.TailnetFallbackLogin,
						Name:  "tailnet (" + auth.TailnetFallbackLogin + ")",
					})
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			if login == "" {
				http.Error(w, "authentication required: use Tailscale identity headers, or Authorization: Bearer <token> if configured, or --tailnet-fallback-login for CGNAT-only ingress (see cnpg-portal README)", http.StatusForbidden)
				return
			}
			id := Identity{
				Login:  login,
				Name:   r.Header.Get(HeaderName),
				PicURL: r.Header.Get(HeaderProfilePic),
			}
			ctx := context.WithValue(r.Context(), identityKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func parseBearerSecret(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	fields := strings.Fields(h)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ""
	}
	return fields[1]
}

func bearerTokensEqual(expected, got string) bool {
	if len(expected) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

// requestClientIP returns the leftmost X-Forwarded-For hop, else X-Real-Ip, else RemoteAddr.
func requestClientIP(r *http.Request) net.IP {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := net.ParseIP(strings.TrimSpace(parts[0])); ip != nil {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		if ip := net.ParseIP(strings.TrimSpace(xri)); ip != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return net.ParseIP(r.RemoteAddr)
	}
	return net.ParseIP(host)
}

// IsTailscaleCGNAT reports whether ip is in 100.64.0.0/10 (Tailscale tailnet addresses).
func IsTailscaleCGNAT(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip = ip.To4()
	if ip == nil {
		return false
	}
	return ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127
}

// AdminList holds the set of Tailscale logins that may invoke mutation
// verbs. Backed by a newline-separated file mounted from a ConfigMap;
// reload happens on SIGHUP so changes go through GitOps without a
// pod restart.
type AdminList struct {
	mu     sync.RWMutex
	logins map[string]struct{}
	path   string
	logger *slog.Logger

	// bearerAdmin is a login that always passes RequireAdmin when bearer token
	// auth is enabled; survives admin-file reload (unlike map-only grants).
	bearerAdmin string
}

// NewAdminList loads the file at path. Empty path is allowed and means
// "no admins configured" — every mutation will 403, which is the right
// fail-closed default until the operator wires the ConfigMap. In Serve,
// when path is empty and --dev-login is set, GrantDevFallback adds that
// login so local development works without a file.
func NewAdminList(path string, logger *slog.Logger) (*AdminList, error) {
	a := &AdminList{path: path, logger: logger, logins: map[string]struct{}{}}
	if path == "" {
		return a, nil
	}
	if err := a.Reload(); err != nil {
		return nil, err
	}
	return a, nil
}

// SetBearerAdmin marks login as always-admin when requests authenticate with
// the configured bearer token. Idempotent; use empty login to clear.
func (a *AdminList) SetBearerAdmin(login string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.bearerAdmin = login
	a.mu.Unlock()
	if login != "" {
		a.logger.Info("admin: bearer token identity is always admin", "login", login)
	}
}

// GrantDevFallback adds login to the allow-list when no --admin-list-path
// was set. Pairs with --dev-login for laptop development only: with an
// empty path, the operator has not configured an allow-list file, so
// treating the dev identity as admin avoids every mutation 403'ing.
// When path is non-empty, the file is authoritative and this is a no-op.
//
// Logged at WARN level because it is a security-relevant behaviour: any
// network-reachable client receives the configured login + admin role.
// In production, set --admin-list-path so this fallback never fires.
func (a *AdminList) GrantDevFallback(login string) {
	if a == nil || a.path != "" || login == "" {
		return
	}
	a.mu.Lock()
	a.logins[login] = struct{}{}
	a.mu.Unlock()
	a.logger.Warn(
		"admin: --admin-list-path not set — every request is auto-admin as the configured --dev-login. Set --admin-list-path in production.",
		"login", login,
	)
}

// Reload re-reads the admin file. Lines starting with `#` are comments,
// blank lines are skipped, surrounding whitespace is trimmed.
func (a *AdminList) Reload() error {
	if a.path == "" {
		return nil
	}
	f, err := os.Open(a.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.mu.Lock()
			a.logins = map[string]struct{}{}
			a.mu.Unlock()
			a.logger.Warn("admin-list file missing — no admins configured", "path", a.path)
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()

	next := map[string]struct{}{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		next[line] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	a.mu.Lock()
	a.logins = next
	a.mu.Unlock()
	a.logger.Info("admin-list reloaded", "path", a.path, "count", len(next))
	return nil
}

// Has reports whether login is on the admin list. Read-locked so SIGHUP
// reload doesn't tear a snapshot.
func (a *AdminList) Has(login string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.bearerAdmin != "" && login == a.bearerAdmin {
		return true
	}
	_, ok := a.logins[login]
	return ok
}

// Count returns the current admin count — used for healthcheck visibility.
func (a *AdminList) Count() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.logins)
}

// WatchSIGHUP reloads the admin file each time the process receives
// SIGHUP. Returns when ctx is done. Safe to call once at startup.
func (a *AdminList) WatchSIGHUP(ctx context.Context) {
	if a.path == "" {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				if err := a.Reload(); err != nil {
					a.logger.Error("admin-list reload", "err", err)
				}
			}
		}
	}()
}

// RequireAdmin gates the wrapped handler to logins on the AdminList.
// Identity must already be on the context (place after IdentityMiddleware
// in the chain). A nil AdminList is treated as "no admins configured" —
// every request 403s, which is the right fail-closed behaviour until the
// operator wires the ConfigMap.
func RequireAdmin(admins *AdminList) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := IdentityFrom(r.Context())
			if !ok {
				http.Error(w, "identity middleware not configured", http.StatusInternalServerError)
				return
			}
			if admins == nil || !admins.Has(id.Login) {
				http.Error(w, "admin required: "+id.Login+" is not on the admin allow-list", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
