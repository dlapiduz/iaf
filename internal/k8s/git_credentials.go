package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// LabelCredentialType is set on every git credential Secret.
	LabelCredentialType = "iaf.io/credential-type"

	// AnnotationGitServer records the server URL on the Secret for informational purposes.
	AnnotationGitServer = "iaf.io/git-server"

	// AnnotationKpackGit is the annotation kpack uses to match a credential to a git URL.
	AnnotationKpackGit = "kpack.io/git"

	// KpackServiceAccount is the name of the kpack SA in each session namespace.
	KpackServiceAccount = "iaf-kpack-sa"
)

// BuildGitCredentialSecret constructs a Kubernetes Secret for kpack git auth.
// credType must be "basic-auth" or "ssh".
// StringData is write-only; the API server converts it to base64 Data; credential
// material is never read back by any IAF tool.
func BuildGitCredentialSecret(namespace, name, credType, gitServerURL, username, password, privateKey string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				LabelCredentialType: "git",
			},
			Annotations: map[string]string{
				AnnotationKpackGit:  gitServerURL,
				AnnotationGitServer: gitServerURL,
			},
		},
	}

	switch credType {
	case "basic-auth":
		secret.Type = corev1.SecretTypeBasicAuth
		secret.StringData = map[string]string{
			"username": username,
			"password": password,
		}
	case "ssh":
		secret.Type = corev1.SecretTypeSSHAuth
		secret.StringData = map[string]string{
			corev1.SSHAuthPrivateKey: privateKey,
		}
	}

	return secret
}

// AddSecretToKpackSA appends secretName to the iaf-kpack-sa ServiceAccount's
// secrets list (required by kpack for credential lookup). Idempotent.
func AddSecretToKpackSA(ctx context.Context, c client.Client, namespace, secretName string) error {
	sa := &corev1.ServiceAccount{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: KpackServiceAccount}, sa); err != nil {
		return fmt.Errorf("getting kpack service account: %w", err)
	}

	for _, ref := range sa.Secrets {
		if ref.Name == secretName {
			return nil // already present
		}
	}

	sa.Secrets = append(sa.Secrets, corev1.ObjectReference{Name: secretName})
	if err := c.Update(ctx, sa); err != nil {
		return fmt.Errorf("updating kpack service account: %w", err)
	}
	return nil
}

// RemoveSecretFromKpackSA removes secretName from the iaf-kpack-sa ServiceAccount's
// secrets list. Idempotent — if not present, it is a no-op.
func RemoveSecretFromKpackSA(ctx context.Context, c client.Client, namespace, secretName string) error {
	sa := &corev1.ServiceAccount{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: KpackServiceAccount}, sa); err != nil {
		return fmt.Errorf("getting kpack service account: %w", err)
	}

	filtered := make([]corev1.ObjectReference, 0, len(sa.Secrets))
	for _, ref := range sa.Secrets {
		if ref.Name != secretName {
			filtered = append(filtered, ref)
		}
	}
	if len(filtered) == len(sa.Secrets) {
		return nil // not present, no-op
	}

	sa.Secrets = filtered
	if err := c.Update(ctx, sa); err != nil {
		return fmt.Errorf("updating kpack service account: %w", err)
	}
	return nil
}
