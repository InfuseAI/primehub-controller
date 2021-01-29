package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ImageCrdSpecImageSpec struct {
	BaseImage  string `json:"baseImage"`
	PullSecret string `json:"pullSecret,omitempty"`

	Packages ImageSpecSpecPackages `json:"packages,omitempty"`

	UpdateTime *metav1.Time `json:"updateTime,omitempty"`
	Cancel     bool         `json:"cancel,omitempty"`
}

// ImageSpec defines the desired state of Image
type ImageCrdSpec struct {
	// Foo is an example field of Image. Edit Image_types.go to remove/update

	Url         string `json:"url,omitempty"`
	UrlForGpu   string `json:"urlForGpu,omitempty"`
	Type        string `json:"type,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	PullSecret  string `json:"pullSecret,omitempty"`
	GroupName   string `json:"groupName,omitempty"`

	ImageSpec ImageCrdSpecImageSpec `json:"imageSpec,omitempty"`
}

// ImageStatus defines the observed state of Image
type ImageStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	JobCondiction ImageSpecStatus `json:"jobCondition"`
}

// +kubebuilder:object:root=true

// Image is the Schema for the images API
type Image struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageCrdSpec `json:"spec,omitempty"`
	Status ImageStatus  `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImageList contains a list of Image
type ImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Image `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Image{}, &ImageList{})
}
