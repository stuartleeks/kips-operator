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

	TargetService TargetService `json:"targetService"`
}

// TargetService represents a service targetted by the ServiceBridge
type TargetService struct {
	Name               string              `json:"name"`
	TargetServicePorts []TargetServicePort `json:"ports"`
}

// TargetServicePort holds the configuration for a targetted port on a service
type TargetServicePort struct {
	Name       string `json:"name"`       // Name of the targetted port
	RemotePort int    `json:"remotePort"` // Remote port to forward to
}

// ServiceBridgeStatus defines the observed state of ServiceBridge
type ServiceBridgeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Message              string `json:"message"`
	Temp                 string `json:"temp"`
	ClientAzbridgeConfig string `json:"clientAzbridgeConfig"`
}

// +kubebuilder:object:root=true
// The line below is the key to updating the status without hitting "the server could not find the requested resource" - see https://github.com/kubernetes-sigs/kubebuilder/blob/a06ec9adbe2f3f4388399697f4cc30ed35fef2dd/docs/book/src/reference/generating-crd.md#status
// +kubebuilder:subresource:status

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
