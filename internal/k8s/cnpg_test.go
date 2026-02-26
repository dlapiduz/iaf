package k8s

import (
	"testing"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func makeManagedService(name, namespace string, plan iafv1alpha1.ServicePlan) *iafv1alpha1.ManagedService {
	return &iafv1alpha1.ManagedService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-uid-123",
		},
		Spec: iafv1alpha1.ManagedServiceSpec{
			Type: "postgres",
			Plan: plan,
		},
	}
}

func TestBuildCNPGCluster_Micro(t *testing.T) {
	svc := makeManagedService("mydb", "iaf-test", iafv1alpha1.ServicePlanMicro)
	obj := BuildCNPGCluster(svc)

	if obj.GetName() != "mydb" {
		t.Errorf("expected name mydb, got %s", obj.GetName())
	}
	if obj.GetNamespace() != "iaf-test" {
		t.Errorf("expected namespace iaf-test, got %s", obj.GetNamespace())
	}

	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}
	if spec["instances"] != int64(1) {
		t.Errorf("micro plan: expected 1 instance, got %v", spec["instances"])
	}
	storage, ok := spec["storage"].(map[string]any)
	if !ok {
		t.Fatal("storage is not a map")
	}
	if storage["size"] != "1Gi" {
		t.Errorf("micro plan: expected storage 1Gi, got %v", storage["size"])
	}

	ownerRefs := obj.GetOwnerReferences()
	if len(ownerRefs) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(ownerRefs))
	}
	if ownerRefs[0].Name != "mydb" {
		t.Errorf("owner ref name: expected mydb, got %s", ownerRefs[0].Name)
	}
	if ownerRefs[0].Kind != "ManagedService" {
		t.Errorf("owner ref kind: expected ManagedService, got %s", ownerRefs[0].Kind)
	}
}

func TestBuildCNPGCluster_HA(t *testing.T) {
	svc := makeManagedService("hadb", "iaf-test", iafv1alpha1.ServicePlanHA)
	obj := BuildCNPGCluster(svc)

	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}
	if spec["instances"] != int64(3) {
		t.Errorf("ha plan: expected 3 instances, got %v", spec["instances"])
	}
	storage, ok := spec["storage"].(map[string]any)
	if !ok {
		t.Fatal("storage is not a map")
	}
	if storage["size"] != "10Gi" {
		t.Errorf("ha plan: expected storage 10Gi, got %v", storage["size"])
	}
}

func TestGetCNPGClusterStatus_Ready(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("mydb")
	obj.Object["status"] = map[string]any{
		"conditions": []any{
			map[string]any{
				"type":   "Ready",
				"status": "True",
			},
		},
	}

	phase, secretName := GetCNPGClusterStatus(obj)
	if phase != string(iafv1alpha1.ManagedServicePhaseReady) {
		t.Errorf("expected Ready phase, got %s", phase)
	}
	if secretName != "mydb-app" {
		t.Errorf("expected secret name mydb-app, got %s", secretName)
	}
}

func TestGetCNPGClusterStatus_Provisioning(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("mydb")
	// No conditions set — still provisioning.

	phase, secretName := GetCNPGClusterStatus(obj)
	if phase != string(iafv1alpha1.ManagedServicePhaseProvisioning) {
		t.Errorf("expected Provisioning phase, got %s", phase)
	}
	if secretName != "mydb-app" {
		t.Errorf("expected secret name mydb-app, got %s", secretName)
	}
}

func TestGetCNPGClusterStatus_ReadyFalse(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetName("db2")
	obj.Object["status"] = map[string]any{
		"conditions": []any{
			map[string]any{
				"type":   "Ready",
				"status": "False",
			},
		},
	}

	phase, _ := GetCNPGClusterStatus(obj)
	if phase != string(iafv1alpha1.ManagedServicePhaseProvisioning) {
		t.Errorf("expected Provisioning phase when Ready=False, got %s", phase)
	}
}

func TestBuildNetworkPolicy(t *testing.T) {
	svc := makeManagedService("mydb", "iaf-test", iafv1alpha1.ServicePlanMicro)
	np := BuildNetworkPolicy(svc)

	if np.Name != "mydb-netpol" {
		t.Errorf("expected name mydb-netpol, got %s", np.Name)
	}
	if np.Namespace != "iaf-test" {
		t.Errorf("expected namespace iaf-test, got %s", np.Namespace)
	}

	labels := np.Spec.PodSelector.MatchLabels
	if labels["cnpg.io/cluster"] != "mydb" {
		t.Errorf("expected cnpg.io/cluster=mydb, got %v", labels)
	}

	if len(np.Spec.Ingress) != 1 {
		t.Fatalf("expected 1 ingress rule, got %d", len(np.Spec.Ingress))
	}
	if len(np.Spec.Ingress[0].From) != 1 {
		t.Fatalf("expected 1 from peer, got %d", len(np.Spec.Ingress[0].From))
	}
	if np.Spec.Ingress[0].From[0].PodSelector == nil {
		t.Error("expected pod selector in ingress rule")
	}

	ownerRefs := np.GetOwnerReferences()
	if len(ownerRefs) != 1 || ownerRefs[0].Kind != "ManagedService" {
		t.Errorf("expected 1 owner ref of kind ManagedService, got %v", ownerRefs)
	}
}
