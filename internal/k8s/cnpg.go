package k8s

import (
	"fmt"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// CNPGClusterGVK is the GroupVersionKind for CloudNativePG Cluster CRs.
var CNPGClusterGVK = schema.GroupVersionKind{
	Group:   "postgresql.cnpg.io",
	Version: "v1",
	Kind:    "Cluster",
}

// PlanConfig holds the resource configuration for a given ServicePlan.
type PlanConfig struct {
	Instances int
	CPU       string
	Memory    string
	StorageGB int
}

// planConfigs maps service plans to their resource configurations.
var planConfigs = map[iafv1alpha1.ServicePlan]PlanConfig{
	iafv1alpha1.ServicePlanMicro: {Instances: 1, CPU: "250m", Memory: "256Mi", StorageGB: 1},
	iafv1alpha1.ServicePlanSmall: {Instances: 1, CPU: "500m", Memory: "512Mi", StorageGB: 5},
	iafv1alpha1.ServicePlanHA:    {Instances: 3, CPU: "1", Memory: "1Gi", StorageGB: 10},
}

// PlanConfigFor returns the PlanConfig for the given ServicePlan.
// Returns false if the plan is not found.
func PlanConfigFor(plan iafv1alpha1.ServicePlan) (PlanConfig, bool) {
	cfg, ok := planConfigs[plan]
	return cfg, ok
}

// BuildCNPGCluster constructs an unstructured CloudNativePG Cluster CR for the given ManagedService.
func BuildCNPGCluster(svc *iafv1alpha1.ManagedService) *unstructured.Unstructured {
	cfg := planConfigs[svc.Spec.Plan]

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(CNPGClusterGVK)
	obj.SetName(svc.Name)
	obj.SetNamespace(svc.Namespace)
	obj.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "iaf",
		"iaf.io/managed-service":      svc.Name,
	})
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: iafv1alpha1.GroupVersion.String(),
			Kind:       "ManagedService",
			Name:       svc.Name,
			UID:        svc.UID,
			Controller: boolPtr(true),
		},
	})

	obj.Object["spec"] = map[string]any{
		"instances": int64(cfg.Instances),
		"storage": map[string]any{
			"size": fmt.Sprintf("%dGi", cfg.StorageGB),
		},
		"resources": map[string]any{
			"requests": map[string]any{
				"cpu":    cfg.CPU,
				"memory": cfg.Memory,
			},
		},
	}
	return obj
}

// GetCNPGClusterStatus reads the phase and connection secret name from a CNPG Cluster CR.
// The secret name follows the CNPG convention: <cluster-name>-app.
func GetCNPGClusterStatus(obj *unstructured.Unstructured) (phase string, secretName string) {
	secretName = obj.GetName() + "-app"

	status, ok := obj.Object["status"].(map[string]any)
	if !ok {
		return string(iafv1alpha1.ManagedServicePhaseProvisioning), secretName
	}

	conditions, ok := status["conditions"].([]any)
	if !ok {
		return string(iafv1alpha1.ManagedServicePhaseProvisioning), secretName
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == "Ready" {
			if cond["status"] == "True" {
				return string(iafv1alpha1.ManagedServicePhaseReady), secretName
			}
			return string(iafv1alpha1.ManagedServicePhaseProvisioning), secretName
		}
	}
	return string(iafv1alpha1.ManagedServicePhaseProvisioning), secretName
}

// BuildNetworkPolicy constructs a NetworkPolicy that allows ingress to CNPG cluster pods
// from pods in the same namespace and from the CNPG operator namespace (cnpg-system).
// The CNPG operator must be able to reach database pods on its internal status port
// (default 8000) to extract health information; blocking it causes the cluster to stay
// in a "not ready" state even when the PostgreSQL process is healthy.
func BuildNetworkPolicy(svc *iafv1alpha1.ManagedService) *networkingv1.NetworkPolicy {
	protocolTCP := corev1.Protocol("TCP")
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name + "-netpol",
			Namespace: svc.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "iaf",
				"iaf.io/managed-service":      svc.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: iafv1alpha1.GroupVersion.String(),
					Kind:       "ManagedService",
					Name:       svc.Name,
					UID:        svc.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"cnpg.io/cluster": svc.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							// Allow all pods in the same namespace (app connectivity).
							PodSelector: &metav1.LabelSelector{},
						},
						{
							// Allow the CNPG operator to reach database pods for
							// health/status checks on its internal communication port.
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "cnpg-system",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &protocolTCP},
					},
				},
			},
		},
	}
}

func boolPtr(b bool) *bool { return &b }
