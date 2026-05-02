package tenant

import (
	"context"
	"errors"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aberti/cnpg-portal/internal/connstr"
	"github.com/aberti/cnpg-portal/internal/pg"
)

// ErrCredentialsNotFound signals the live K8s Secret backing this tenant is
// missing. Distinct from ErrTenantNotFound because the database can exist
// without its credentials Secret in degraded states (e.g. ArgoCD sync mid-flight).
var ErrCredentialsNotFound = errors.New("credentials secret not found")

// Credentials is the live connection bundle: the cluster coordinates plus
// the password decrypted from the in-cluster Secret.
type Credentials struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

// Inputs converts to the rendering pkg's input shape. We keep the two
// types separate (rather than aliasing) because connstr is intentionally
// independent of any tenant/k8s concept — it's pure formatting — and the
// adapter is a single field-for-field copy.
func (c Credentials) Inputs() connstr.Inputs {
	return connstr.Inputs{
		Host:     c.Host,
		Port:     c.Port,
		Database: c.Database,
		User:     c.User,
		Password: c.Password,
	}
}

// inClusterPort is the CNPG primary's read-write Service port. Hard-coded
// to match cluster.yaml; the day we run a non-default port we'll lift this
// off the Cluster CR.
const inClusterPort = 5432

// ReadCredentials fetches the live Secret <app>-pg-credentials from the
// CNPG namespace and assembles a Credentials. The DB/role/host are derived
// from the tenant name + workspace.Namespace; only the password comes from
// the Secret. RBAC must grant `secrets get` on the namespace.
func ReadCredentials(ctx context.Context, d Deps, app string) (*Credentials, error) {
	if !pg.IdentSafe(app) {
		return nil, fmt.Errorf("invalid tenant name %q", app)
	}
	if d.K8s == nil || d.K8s.Clientset == nil {
		return nil, errors.New("ReadCredentials: K8s dep is required")
	}
	if d.Workspace == nil {
		return nil, errors.New("ReadCredentials: Workspace dep is required")
	}

	ns := d.Workspace.Namespace
	secretName := K8sSecretName(app)
	sec, err := d.K8s.Clientset.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, ErrCredentialsNotFound
		}
		return nil, fmt.Errorf("get secret %s/%s: %w", ns, secretName, err)
	}
	pw, ok := sec.Data["password"]
	if !ok || len(pw) == 0 {
		return nil, fmt.Errorf("secret %s/%s has no `password` key", ns, secretName)
	}

	return &Credentials{
		Host:     fmt.Sprintf("%s-rw.%s.svc.cluster.local", d.Workspace.ServiceName, ns),
		Port:     inClusterPort,
		Database: app,
		User:     app,
		Password: string(pw),
	}, nil
}
