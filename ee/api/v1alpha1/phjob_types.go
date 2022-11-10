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

// +kubebuilder:validation:Enum=Job
type JobType string

// +kubebuilder:validation:Enum=Pending;Preparing;Running;Succeeded;Failed;Cancelled;Unknown
type PhJobPhase string

const (
	JobPending   PhJobPhase = "Pending"
	JobPreparing PhJobPhase = "Preparing"
	JobRunning   PhJobPhase = "Running"
	JobSucceeded PhJobPhase = "Succeeded"
	JobFailed    PhJobPhase = "Failed"
	JobCancelled PhJobPhase = "Cancelled"
	JobUnknown   PhJobPhase = "Unknown"
)

type PhJobReason string

const (
	JobReasonOverRequeueLimit     PhJobReason = "OverRequeueLimit"
	JobReasonOverActivateDeadline PhJobReason = "OverActivateDeadline"
	JobReasonOverQuota            PhJobReason = "OverQuota"
	JobReasonCancelled            PhJobReason = "Cancelled"
	JobReasonPodCreationFailed    PhJobReason = "PodCreationFailed"
	JobReasonPodFailed            PhJobReason = "PodFailed"
	JobReasonPodUnknown           PhJobReason = "PodUnknown"
	JobReasonPodPending           PhJobReason = "PodPending"
	JobReasonPodRunning           PhJobReason = "PodRunning"
	JobReasonPodSucceeded         PhJobReason = "PodSucceeded"
)

// PhJobSpec defines the desired state of PhJob
type PhJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	DisplayName             string `json:"displayName"`
	UserId                  string `json:"userId"`
	UserName                string `json:"userName,omitempty"`
	GroupId                 string `json:"groupId"`
	GroupName               string `json:"groupName"`
	InstanceType            string `json:"instanceType"`
	Image                   string `json:"image"`
	Command                 string `json:"command"`
	Cancel                  bool   `json:"cancel,omitempty"`
	RequeueLimit            *int32 `json:"requeueLimit,omitempty"`
	ActiveDeadlineSeconds   *int64 `json:"activeDeadlineSeconds,omitempty"`
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// PhJobStatus defines the observed state of PhJob
type PhJobStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase      PhJobPhase   `json:"phase"`
	Reason     PhJobReason  `json:"reason,omitempty"`
	Message    string       `json:"message,omitempty"`
	PodName    string       `json:"podName,omitempty"`
	StartTime  *metav1.Time `json:"startTime,omitempty"`
	FinishTime *metav1.Time `json:"finishTime,omitempty"`
	Requeued   *int32       `json:"requeued,omitempty"`
}

type PhJobTemplateSpec struct {
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Spec              PhJobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="User",type="string",JSONPath=".spec.userName"
// +kubebuilder:printcolumn:name="Group",type="string",JSONPath=".spec.groupName"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="Status of the job"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

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
