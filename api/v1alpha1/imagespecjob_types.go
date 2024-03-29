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

// ImageSpecJobSpec defines the desired state of ImageSpecJob
type ImageSpecJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	BaseImage  string `json:"baseImage"`
	PullSecret string `json:"pullSecret,omitempty"`

	Packages    ImageSpecSpecPackages `json:"packages"`
	TargetImage string                `json:"targetImage"`
	PushSecret  string                `json:"pushSecret"`
	RepoPrefix  string                `json:"repoPrefix,omitempty"`

	UpdateTime *metav1.Time `json:"updateTime,omitempty"`
}

// ImageSpecJobStatus defines the observed state of ImageSpecJob
type ImageSpecJobStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase      string       `json:"phase"`
	StartTime  *metav1.Time `json:"startTime,omitempty"`
	FinishTime *metav1.Time `json:"finishTime,omitempty"`
	PodName    string       `json:"podName"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="status of current job"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ImageSpecJob is the Schema for the imagespecjobs API
type ImageSpecJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageSpecJobSpec   `json:"spec,omitempty"`
	Status ImageSpecJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImageSpecJobList contains a list of ImageSpecJob
type ImageSpecJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImageSpecJob `json:"items"`
}

func (imageSpecJob *ImageSpecJob) TargetImageURL() (targetImage string) {
	if len(imageSpecJob.Spec.RepoPrefix) > 0 {
		targetImage = imageSpecJob.Spec.RepoPrefix + "/" + imageSpecJob.Spec.TargetImage
	} else {
		targetImage = imageSpecJob.Spec.TargetImage
	}
	return
}
func init() {
	SchemeBuilder.Register(&ImageSpecJob{}, &ImageSpecJobList{})
}
