package k8s

import (
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TraefikIngressRouteGVK is the GroupVersionKind for Traefik IngressRoute CRs.
var TraefikIngressRouteGVK = schema.GroupVersionKind{
	Group:   "traefik.io",
	Version: "v1alpha1",
	Kind:    "IngressRoute",
}

// TraefikIngressRouteGVR is the GroupVersionResource for Traefik IngressRoute CRs.
var TraefikIngressRouteGVR = schema.GroupVersionResource{
	Group:    "traefik.io",
	Version:  "v1alpha1",
	Resource: "ingressroutes",
}

// BuildIngressRoute constructs an unstructured Traefik IngressRoute for the given application.
func BuildIngressRoute(app *iafv1alpha1.Application, baseDomain string) *unstructured.Unstructured {
	host := app.Spec.Host
	if host == "" {
		host = fmt.Sprintf("%s.%s", app.Name, baseDomain)
	}

	port := app.Spec.Port
	if port == 0 {
		port = 8080
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(TraefikIngressRouteGVK)
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
		"entryPoints": []any{"web"},
		"routes": []any{
			map[string]any{
				"match": fmt.Sprintf("Host(`%s`)", host),
				"kind":  "Rule",
				"services": []any{
					map[string]any{
						"name": app.Name,
						"port": int64(port),
					},
				},
			},
		},
	}

	return obj
}
