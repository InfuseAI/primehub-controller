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

// +kubebuilder:validation:Enum=Job
type JobType string

const (
	// Job is kubernetes job.
	KubernetesJob JobType = "Job"
)

// +kubebuilder:validation:Enum=Job
type PhJobPhase string

const (
	JobPending   PhJobPhase = "Pending"
	JobRunning   PhJobPhase = "Running"
	JobSucceeded PhJobPhase = "Succeeded"
	JobFailed    PhJobPhase = "Failed"
	JobUnknown   PhJobPhase = "Unknown"
)

// PhJobSpec defines the desired state of PhJob
type PhJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	JobType JobType `json:"jobType"`

	User         string   `json:"user"`
	Group        string   `json:"group"`
	InstanceType string   `json:"instanceType"`
	Image        string   `json:"image"`
	Command      []string `json:"command"`

	// ----------- From JobSpec -----------
	// Specifies the duration in seconds relative to the startTime that the job may be active
	// before the system tries to terminate it; value must be positive integer
	// +optional
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty" protobuf:"varint,3,opt,name=activeDeadlineSeconds"`
	// Specifies the number of retries before marking this job failed.
	// Defaults to 6
	// +optional
	BackoffLimit *int32 `json:"backoffLimit,omitempty" protobuf:"varint,7,opt,name=backoffLimit"`
	// ttlSecondsAfterFinished limits the lifetime of a Job that has finished
	// execution (either Complete or Failed). If this field is set,
	// ttlSecondsAfterFinished after the Job finishes, it is eligible to be
	// automatically deleted. When the Job is being deleted, its lifecycle
	// guarantees (e.g. finalizers) will be honored. If this field is unset,
	// the Job won't be automatically deleted. If this field is set to zero,
	// the Job becomes eligible to be deleted immediately after it finishes.
	// This field is alpha-level and is only honored by servers that enable the
	// TTLAfterFinished feature.
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty" protobuf:"varint,8,opt,name=ttlSecondsAfterFinished"`
}

// PhJobStatus defines the observed state of PhJob
type PhJobStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase      PhJobPhase   `json:"phase"`
	StartTime  *metav1.Time `json:"startTime,omitempty"`
	FinishTime *metav1.Time `json:"finishTime,omitempty"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true

// PhJob is the Schema for the phjobs API
type PhJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PhJobSpec   `json:"spec,omitempty"`
	Status PhJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PhJobList contains a list of PhJob
type PhJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PhJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PhJob{}, &PhJobList{})
}
