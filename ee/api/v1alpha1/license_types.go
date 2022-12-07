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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LicenseSpec defines the desired state of License
type LicenseSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	SignedLicense string `json:"signed_license"`
}

// LicenseStatus defines the observed state of License
type LicenseStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Expired        string `json:"expired,omitempty"`
	Reason         string `json:"reason,omitempty"`
	LicensedTo     string `json:"licensed_to,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	ExpiredAt      string `json:"expired_at,omitempty"`
	MaxUser        *int   `json:"max_user,omitempty"`
	MaxGroup       *int   `json:"max_group,omitempty"`
	MaxNode        *int   `json:"max_node,omitempty"`
	MaxModelDeploy *int   `json:"max_model_deploy,omitempty"`
	PlatformType   string `json:"platform_type,omitempty"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true

// License is the Schema for the licenses API
type License struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LicenseSpec   `json:"spec,omitempty"`
	Status LicenseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LicenseList contains a list of License
type LicenseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []License `json:"items"`
}

func init() {
	SchemeBuilder.Register(&License{}, &LicenseList{})
}
