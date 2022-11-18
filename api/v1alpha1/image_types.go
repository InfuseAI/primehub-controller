/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="type of current image"
// +kubebuilder:printcolumn:name="Group",type="string",JSONPath=".spec.groupName",description="group of current image"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.jobCondition.phase",description="phase of current image"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

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
