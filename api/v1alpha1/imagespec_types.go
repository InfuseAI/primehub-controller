/*

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
	PullSecret string `json:"pullSecret"`

	Packages ImageSpecSpecPackages `json:"packages"`
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
