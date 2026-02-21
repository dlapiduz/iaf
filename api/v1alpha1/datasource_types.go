package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// DataSourceSpec defines the configuration for a platform-managed data source.
type DataSourceSpec struct {
	// Kind is the type of data source (e.g., postgres, mysql, s3, http-api, kafka).
	Kind string `json:"kind"`

	// Description is a human-readable description of the data source.
	// +optional
	Description string `json:"description,omitempty"`

	// Schema is an optional description of the data schema (e.g., table names, API endpoints).
	// +optional
	Schema string `json:"schema,omitempty"`

	// Tags are optional labels for filtering and categorising data sources.
	// +optional
	Tags []string `json:"tags,omitempty"`

	// SecretRef references the operator-created Secret containing connection credentials.
	// Agents cannot change this field — only operators can register DataSources.
	SecretRef DataSourceSecretRef `json:"secretRef"`

	// EnvVarMapping maps Secret data keys to the environment variable names injected
	// into applications that attach this DataSource.
	// Key: a key in the referenced Secret. Value: the env var name in the application container.
	EnvVarMapping map[string]string `json:"envVarMapping"`
}

// DataSourceSecretRef identifies a Kubernetes Secret in a specific namespace.
type DataSourceSecretRef struct {
	// Name is the name of the Secret.
	Name string `json:"name"`

	// Namespace is the namespace containing the Secret (typically iaf-system).
	Namespace string `json:"namespace"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=`.spec.kind`
// +kubebuilder:printcolumn:name="Description",type=string,JSONPath=`.spec.description`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DataSource is the Schema for the datasources API.
// DataSources are registered by platform operators via kubectl and surfaced
// to agents via the list_data_sources, get_data_source, and attach_data_source MCP tools.
// Agents cannot create or modify DataSources.
type DataSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DataSourceSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// DataSourceList contains a list of DataSource.
type DataSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataSource{}, &DataSourceList{})
}
