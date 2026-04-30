package k8s

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecOptions configures a single remote-exec call against a Pod.
type ExecOptions struct {
	Namespace string
	Pod       string
	Container string   // empty → pod's default container
	Command   []string // argv passed to /sbin/init's exec, e.g. ["psql", "-U", "postgres"]
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	TTY       bool
}

// Exec runs Command inside the named Pod/Container via the Kubernetes API
// (SPDY transport), streaming Stdin/Stdout/Stderr through the connection.
// It is the equivalent of `kubectl exec -i <pod> -c <container> -- <cmd>`.
func (c *Client) Exec(ctx context.Context, opts ExecOptions) error {
	if opts.Namespace == "" || opts.Pod == "" {
		return fmt.Errorf("exec: namespace and pod are required")
	}
	if len(opts.Command) == 0 {
		return fmt.Errorf("exec: command is required")
	}

	req := c.Clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(opts.Pod).
		Namespace(opts.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: opts.Container,
			Command:   opts.Command,
			Stdin:     opts.Stdin != nil,
			Stdout:    opts.Stdout != nil,
			Stderr:    opts.Stderr != nil,
			TTY:       opts.TTY,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.RestConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("build SPDY executor: %w", err)
	}

	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
		Tty:    opts.TTY,
	})
}
