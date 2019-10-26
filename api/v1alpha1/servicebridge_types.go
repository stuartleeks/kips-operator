package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ServiceBridgeSpec defines the desired state of ServiceBridge
type ServiceBridgeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// ServiceBridgeStatus defines the observed state of ServiceBridge
type ServiceBridgeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true

// ServiceBridge is the Schema for the servicebridges API
type ServiceBridge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceBridgeSpec   `json:"spec,omitempty"`
	Status ServiceBridgeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceBridgeList contains a list of ServiceBridge
type ServiceBridgeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceBridge `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceBridge{}, &ServiceBridgeList{})
}
