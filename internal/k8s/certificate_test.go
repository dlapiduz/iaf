package k8s

import (
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeTestApp(name, namespace string) *iafv1alpha1.Application {
	return &iafv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-uid",
		},
		Spec: iafv1alpha1.ApplicationSpec{
			Port: 8080,
		},
	}
}

func TestBuildCertificate(t *testing.T) {
	app := makeTestApp("my-app", "iaf-abc123")
	host := "my-app.example.com"
	issuer := "selfsigned-issuer"

	cert := BuildCertificate(app, host, issuer)

	if cert.GetName() != "my-app" {
		t.Errorf("expected name 'my-app', got %q", cert.GetName())
	}
	if cert.GetNamespace() != "iaf-abc123" {
		t.Errorf("expected namespace 'iaf-abc123', got %q", cert.GetNamespace())
	}
	if cert.GroupVersionKind() != CertificateGVK {
		t.Errorf("expected GVK %v, got %v", CertificateGVK, cert.GroupVersionKind())
	}

	spec, _ := cert.Object["spec"].(map[string]any)
	if spec["secretName"] != "my-app-tls" {
		t.Errorf("expected secretName 'my-app-tls', got %v", spec["secretName"])
	}

	dnsNames, _ := spec["dnsNames"].([]any)
	if len(dnsNames) != 1 || dnsNames[0] != host {
		t.Errorf("expected dnsNames [%q], got %v", host, dnsNames)
	}

	issuerRef, _ := spec["issuerRef"].(map[string]any)
	if issuerRef["name"] != "selfsigned-issuer" {
		t.Errorf("expected issuerRef.name 'selfsigned-issuer', got %v", issuerRef["name"])
	}
	if issuerRef["kind"] != "ClusterIssuer" {
		t.Errorf("expected issuerRef.kind 'ClusterIssuer', got %v", issuerRef["kind"])
	}

	// Owner reference must point to the Application.
	ownerRefs := cert.GetOwnerReferences()
	if len(ownerRefs) != 1 || ownerRefs[0].Name != "my-app" {
		t.Errorf("expected owner reference to Application 'my-app', got %v", ownerRefs)
	}
}

func TestTLSSecretName(t *testing.T) {
	if got := TLSSecretName("my-app"); got != "my-app-tls" {
		t.Errorf("expected 'my-app-tls', got %q", got)
	}
}

func TestBuildIngressRoute_TLS(t *testing.T) {
	app := makeTestApp("my-app", "iaf-abc123")
	route := BuildIngressRoute(app, "example.com", true)

	spec, _ := route.Object["spec"].(map[string]any)
	entryPoints, _ := spec["entryPoints"].([]any)
	if len(entryPoints) != 1 || entryPoints[0] != "websecure" {
		t.Errorf("expected entryPoints [websecure], got %v", entryPoints)
	}

	tls, _ := spec["tls"].(map[string]any)
	if tls["secretName"] != "my-app-tls" {
		t.Errorf("expected tls.secretName 'my-app-tls', got %v", tls["secretName"])
	}
}

func TestBuildIngressRoute_NoTLS(t *testing.T) {
	app := makeTestApp("my-app", "iaf-abc123")
	route := BuildIngressRoute(app, "example.com", false)

	spec, _ := route.Object["spec"].(map[string]any)
	entryPoints, _ := spec["entryPoints"].([]any)
	if len(entryPoints) != 1 || entryPoints[0] != "web" {
		t.Errorf("expected entryPoints [web], got %v", entryPoints)
	}

	if _, hasTLS := spec["tls"]; hasTLS {
		t.Error("expected no tls field when TLS is disabled")
	}
}
