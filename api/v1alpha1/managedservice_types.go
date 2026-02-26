package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagedServicePhase represents the lifecycle phase of a ManagedService.
type ManagedServicePhase string

const (
	// ManagedServicePhaseProvisioning indicates the service is being created.
	ManagedServicePhaseProvisioning ManagedServicePhase = "Provisioning"
	// ManagedServicePhaseReady indicates the service is available for use.
	ManagedServicePhaseReady ManagedServicePhase = "Ready"
	// ManagedServicePhaseFailed indicates the service encountered an error.
	ManagedServicePhaseFailed ManagedServicePhase = "Failed"
	// ManagedServicePhaseDeleting indicates the service is being deleted.
	ManagedServicePhaseDeleting ManagedServicePhase = "Deleting"
)

// ServicePlan represents the resource tier for a managed service.
type ServicePlan string

const (
	// ServicePlanMicro is a minimal single-instance plan for development.
	ServicePlanMicro ServicePlan = "micro"
	// ServicePlanSmall is a single-instance plan for light production workloads.
	ServicePlanSmall ServicePlan = "small"
	// ServicePlanHA is a three-instance high-availability plan.
	ServicePlanHA ServicePlan = "ha"
)

// ManagedServiceSpec defines the desired state of a ManagedService.
type ManagedServiceSpec struct {
	// Type is the type of managed service. Currently only "postgres" is supported.
	// +kubebuilder:validation:Enum=postgres
	Type string `json:"type"`

	// Plan is the resource tier: micro, small, or ha.
	// +kubebuilder:validation:Enum=micro;small;ha
	Plan ServicePlan `json:"plan"`
}

// ManagedServiceStatus defines the observed state of a ManagedService.
type ManagedServiceStatus struct {
	// Phase is the current lifecycle phase of the service.
	// +optional
	Phase ManagedServicePhase `json:"phase,omitempty"`

	// Message is a human-readable status message.
	// +optional
	Message string `json:"message,omitempty"`

	// ConnectionSecretRef is the name of the Kubernetes Secret containing connection credentials.
	// Only set when phase is Ready. Never surfaced directly to agents — use bind_service instead.
	// +optional
	ConnectionSecretRef string `json:"connectionSecretRef,omitempty"`

	// BoundApps is the list of Application names that have been bound to this service.
	// The finalizer deletion guard prevents deletion when this list is non-empty.
	// +optional
	BoundApps []string `json:"boundApps,omitempty"`

	// Conditions represent the latest available observations of the service's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Plan",type=string,JSONPath=`.spec.plan`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ManagedService is the Schema for the managedservices API.
type ManagedService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedServiceSpec   `json:"spec,omitempty"`
	Status ManagedServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedServiceList contains a list of ManagedService.
type ManagedServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedService{}, &ManagedServiceList{})
}
