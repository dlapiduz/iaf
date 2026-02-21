package k8s

import (
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// CertificateGVK is the GroupVersionKind for cert-manager Certificate CRs.
var CertificateGVK = schema.GroupVersionKind{
	Group:   "cert-manager.io",
	Version: "v1",
	Kind:    "Certificate",
}

// CertificateGVR is the GroupVersionResource for cert-manager Certificate CRs.
var CertificateGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "certificates",
}

// BuildCertificate constructs an unstructured cert-manager Certificate CR for
// the given application. The Certificate requests a cert for host from issuerName
// (a ClusterIssuer), stores the TLS Secret as "{app.Name}-tls", and is owned
// by the Application so it is garbage-collected on app deletion.
func BuildCertificate(app *iafv1alpha1.Application, host, issuerName string) *unstructured.Unstructured {
	secretName := fmt.Sprintf("%s-tls", app.Name)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(CertificateGVK)
	obj.SetName(app.Name)
	obj.SetNamespace(app.Namespace)
	obj.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "iaf",
		"iaf.io/application":          app.Name,
	})
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: iafv1alpha1.GroupVersion.String(),
			Kind:       "Application",
			Name:       app.Name,
			UID:        app.UID,
		},
	})

	obj.Object["spec"] = map[string]any{
		"secretName": secretName,
		"dnsNames":   []any{host},
		"issuerRef": map[string]any{
			"name": issuerName,
			"kind": "ClusterIssuer",
		},
	}

	return obj
}

// TLSSecretName returns the name of the TLS Secret cert-manager will create
// for the given application.
func TLSSecretName(appName string) string {
	return fmt.Sprintf("%s-tls", appName)
}
