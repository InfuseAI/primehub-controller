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

// PhAppTemplateSpec defines the desired state of PhAppTemplate
type PhAppTemplateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Name        string                    `json:"name"`
	Description string                    `json:"description,omitempty"`
	Version     string                    `json:"version,omitempty"`
	DocLink     string                    `json:"docLink,omitempty"`
	Icon        string                    `json:"icon,omitempty"`
	DefaultEnvs []PhAppTemplateDefaultEnv `json:"defaultEnvs,omitempty"`
	Template    PhAppTemplateContent      `json:"template"`
}

// PhAppTemplateDefaultEnv defines the PhApplication default envs
type PhAppTemplateDefaultEnv struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
	Optional     bool   `json:"optional,omitempty"`
}

type PhAppTemplateContentSpec struct {
	PodTemplate PhApplicationPodTemplate `json:"podTemplate"`
	SvcTemplate PhApplicationSvcTemplate `json:"svcTemplate"`
	HTTPPort    *int32                   `json:"httpPort,omitempty"`
	Rewrite     bool                     `json:"rewrite,omitempty"`
	AppRoot     string                   `json:"appRoot,omitempty"`
}

// PhAppTemplateContent defines the PhApplication spec content
type PhAppTemplateContent struct {
	Spec PhAppTemplateContentSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// PhAppTemplate is the Schema for the phapptemplates API
type PhAppTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PhAppTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// PhAppTemplateList contains a list of PhAppTemplate
type PhAppTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PhAppTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PhAppTemplate{}, &PhAppTemplateList{})
}
