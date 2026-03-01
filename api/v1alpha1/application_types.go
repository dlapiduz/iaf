package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TLSConfig controls HTTPS for an Application.
type TLSConfig struct {
	// Enabled controls whether TLS is provisioned for this application.
	// When nil or true, TLS is enabled (default on). Set to false to opt out.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// IsTLSEnabled returns true when TLS should be enabled for the given application.
// TLS is on by default; set spec.tls.enabled=false to opt out.
func IsTLSEnabled(app *Application) bool {
	if app.Spec.TLS == nil || app.Spec.TLS.Enabled == nil {
		return true
	}
	return *app.Spec.TLS.Enabled
}

// ApplicationSpec defines the desired state of an Application.
type ApplicationSpec struct {
	// Image is a pre-built container image reference (e.g., "nginx:latest").
	// Mutually exclusive with Git and Blob.
	// +optional
	Image string `json:"image,omitempty"`

	// Git specifies a git repository to build from using kpack.
	// Mutually exclusive with Image and Blob.
	// +optional
	Git *GitSource `json:"git,omitempty"`

	// Blob is a URL to a source code archive (tarball) to build from using kpack.
	// Set by the platform when source code is uploaded.
	// Mutually exclusive with Image and Git.
	// +optional
	Blob string `json:"blob,omitempty"`

	// Port is the container port the application listens on.
	// +kubebuilder:default=8080
	// +optional
	Port int32 `json:"port,omitempty"`

	// Replicas is the desired number of pod replicas.
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Env specifies environment variables for the application container.
	// +optional
	Env []EnvVar `json:"env,omitempty"`

	// Host is the hostname for routing. Defaults to "{name}.localhost".
	// +optional
	Host string `json:"host,omitempty"`

	// TLS configures HTTPS for this application. TLS is enabled by default.
	// Set tls.enabled=false to opt out and serve over HTTP only.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`

	// AttachedDataSources lists data sources attached to this application.
	// The controller injects credentials from each DataSource as env vars into the Deployment.
	// Use the attach_data_source MCP tool to add entries here.
	// +optional
	AttachedDataSources []AttachedDataSource `json:"attachedDataSources,omitempty"`

	// BoundManagedServices lists managed services bound to this application.
	// The controller injects connection credentials from each service as env vars into the Deployment.
	// Use the bind_service MCP tool to add entries here.
	// +optional
	BoundManagedServices []BoundManagedService `json:"boundManagedServices,omitempty"`
}

// AttachedDataSource records a DataSource attached to an Application.
type AttachedDataSource struct {
	// DataSourceName is the name of the cluster-scoped DataSource CR.
	DataSourceName string `json:"dataSourceName"`

	// SecretName is the name of the credential Secret copied into the session namespace.
	// This Secret is owned by the Application CR and is deleted when the application is deleted.
	SecretName string `json:"secretName"`
}

// GitSource specifies a git repository source for building.
type GitSource struct {
	// URL is the git repository URL.
	URL string `json:"url"`

	// Revision is the branch, tag, or commit to build. Defaults to "main".
	// +kubebuilder:default="main"
	// +optional
	Revision string `json:"revision,omitempty"`
}

// EnvVar represents an environment variable.
type EnvVar struct {
	// Name of the environment variable.
	Name string `json:"name"`
	// Value of the environment variable.
	// +optional
	Value string `json:"value,omitempty"`
}

// BoundManagedService records a managed service bound to an Application.
// The controller injects PG* connection env vars from the referenced Secret into the Deployment.
type BoundManagedService struct {
	// ServiceName is the name of the ManagedService CR.
	ServiceName string `json:"serviceName"`
	// SecretName is the name of the CNPG connection Secret in the same namespace.
	SecretName string `json:"secretName"`
}

// ApplicationPhase represents the current lifecycle phase of an Application.
type ApplicationPhase string

const (
	ApplicationPhasePending   ApplicationPhase = "Pending"
	ApplicationPhaseBuilding  ApplicationPhase = "Building"
	ApplicationPhaseDeploying ApplicationPhase = "Deploying"
	ApplicationPhaseRunning   ApplicationPhase = "Running"
	ApplicationPhaseFailed    ApplicationPhase = "Failed"
)

// ApplicationStatus defines the observed state of an Application.
type ApplicationStatus struct {
	// Phase is the current lifecycle phase of the application.
	// +optional
	Phase ApplicationPhase `json:"phase,omitempty"`

	// URL is the routable URL for the application.
	// +optional
	URL string `json:"url,omitempty"`

	// LatestImage is the most recently built or provided container image.
	// +optional
	LatestImage string `json:"latestImage,omitempty"`

	// BuildStatus is the kpack build status: Building, Succeeded, or Failed.
	// +optional
	BuildStatus string `json:"buildStatus,omitempty"`

	// AvailableReplicas is the number of available pod replicas.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`

	// Conditions represent the latest available observations of the application's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.status.latestImage`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Application is the Schema for the applications API.
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationSpec   `json:"spec,omitempty"`
	Status ApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApplicationList contains a list of Application.
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Application{}, &ApplicationList{})
}
