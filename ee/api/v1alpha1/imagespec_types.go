package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ImageSpecSpecPackages struct {
	Apt   []string `json:"apt,omitempty"`
	Pip   []string `json:"pip,omitempty"`
	Conda []string `json:"conda,omitempty"`
}

// ImageSpecSpec defines the desired state of ImageSpec
type ImageSpecSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	BaseImage  string `json:"baseImage"`
	PullSecret string `json:"pullSecret,omitempty"`

	Packages ImageSpecSpecPackages `json:"packages"`

	UpdateTime *metav1.Time `json:"updateTime,omitempty"`
}

// ImageSpecStatus defines the observed state of ImageSpec
type ImageSpecStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase   string `json:"phase"`
	JobName string `json:"jobName"`
	Image   string `json:"image"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="status of current job"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ImageSpec is the Schema for the imagespecs API
type ImageSpec struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageSpecSpec   `json:"spec,omitempty"`
	Status ImageSpecStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImageSpecList contains a list of ImageSpec
type ImageSpecList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImageSpec `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImageSpec{}, &ImageSpecList{})
}
