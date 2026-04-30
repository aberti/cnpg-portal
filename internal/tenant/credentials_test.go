package tenant

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekube "k8s.io/client-go/kubernetes/fake"

	"github.com/aberti/cnpg-portal/internal/k8s"
	"github.com/aberti/cnpg-portal/internal/workspace"
)

func depsWithSecrets(secrets ...corev1.Secret) Deps {
	objs := make([]runtime.Object, 0, len(secrets))
	for i := range secrets {
		objs = append(objs, &secrets[i])
	}
	return Deps{
		K8s: &k8s.Client{
			Clientset: fakekube.NewClientset(objs...),
		},
		Workspace: &workspace.Workspace{Namespace: "pg"},
	}
}

func TestReadCredentialsHappy(t *testing.T) {
	d := depsWithSecrets(corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-pg-credentials", Namespace: "pg"},
		Data:       map[string][]byte{"password": []byte("hunter2")},
	})
	c, err := ReadCredentials(context.Background(), d, "acme")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if c.Password != "hunter2" {
		t.Errorf("password mismatch: got %q", c.Password)
	}
	if c.Database != "acme" || c.User != "acme" {
		t.Errorf("db/user mismatch: %+v", c)
	}
	if c.Host != "pg-primary-rw.pg.svc.cluster.local" {
		t.Errorf("host mismatch: %q", c.Host)
	}
	if c.Port != 5432 {
		t.Errorf("port mismatch: %d", c.Port)
	}
}

func TestReadCredentialsNotFound(t *testing.T) {
	d := depsWithSecrets() // no secrets
	_, err := ReadCredentials(context.Background(), d, "acme")
	if !errors.Is(err, ErrCredentialsNotFound) {
		t.Errorf("expected ErrCredentialsNotFound, got %v", err)
	}
}

func TestReadCredentialsMissingPasswordKey(t *testing.T) {
	d := depsWithSecrets(corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-pg-credentials", Namespace: "pg"},
		Data:       map[string][]byte{"other": []byte("x")},
	})
	_, err := ReadCredentials(context.Background(), d, "acme")
	if err == nil || errors.Is(err, ErrCredentialsNotFound) {
		t.Errorf("expected non-nil non-NotFound err, got %v", err)
	}
}

func TestReadCredentialsRejectsBadIdent(t *testing.T) {
	d := depsWithSecrets()
	_, err := ReadCredentials(context.Background(), d, "evil; DROP")
	if err == nil {
		t.Error("expected err for invalid ident")
	}
}
