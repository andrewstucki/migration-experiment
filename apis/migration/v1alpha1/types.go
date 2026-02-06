package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Image struct {
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type Old struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OldSpec   `json:"spec,omitempty"`
	Status OldStatus `json:"status,omitempty"`
}

type OldSpec struct {
	Replicas *int32 `json:"replicas,omitempty"`
}

type OldStatus struct {
	// Add status fields here
}

// +kubebuilder:object:root=true

type OldList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Old `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type New struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NewSpec   `json:"spec,omitempty"`
	Status NewStatus `json:"status,omitempty"`
}

type NewSpec struct {
	Replicas *int32 `json:"replicas,omitempty"`
}

type NewStatus struct {
	// Add status fields here
}

// +kubebuilder:object:root=true

type NewList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []New `json:"items"`
}
