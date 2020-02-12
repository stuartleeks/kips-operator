package v1alpha1

import (
	"faux.ninja/kips-operator/pkg/retry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ServiceBridgeSpec defines the desired state of ServiceBridge
type ServiceBridgeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// TargetService is the service to redirect to the remote connection
	TargetService TargetService `json:"targetService"`
	// AdditionalServices are the services to redirect from the remote connection
	AdditionalServices []AdditionalService `json:"additionalServices"`
}

// TargetService represents a service targeted by the ServiceBridge
type TargetService struct {
	// Name is the name of the service to redirect to the remote connection
	Name string `json:"name"`
	// RelayName is the name of the Azure Relay to connect via
	RelayName string `json:"relayName"`
	// Ports is a collection of ports to redirect
	Ports []TargetServicePort `json:"ports"`
}

// TargetServicePort holds the configuration for a targeted port on a service
type TargetServicePort struct {
	// Name of the targeted port
	Name string `json:"name"`
	// RemotePort is the remote port to forward to
	RemotePort int `json:"remotePort"`
}

// AdditionalService holds the configuration for services that we want the remote machine to be able to forward to
type AdditionalService struct {
	// Name of the service to redirect from the remote connection
	Name string `json:"name"`
	// RelayName is the name of the Azure Relay to connect via
	RelayName string `json:"relayName"`
	// Ports is a collection of ports to redirect
	Ports []AdditionalServicePort `json:"ports"`
}

// AdditionalServicePort holds the configuration for a port on an AdditionalService
type AdditionalServicePort struct {
	// Name is the name of the service to redirect from the remote connection
	Name string `json:"name"` // Name of the targeted port
	// RemotePort is the port to use on the remote machine to route traffic to this service
	RemotePort int `json:"remotePort"`
	// TODO - allow referencing additional services across namespaces
}

// ServiceBridgeStatus defines the observed state of ServiceBridge
type ServiceBridgeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// State gives a high level indication of the ServiceBridge state
	State *ServiceBridgeState `json:"state,omitempty"`
	// ClientAzbridgeConfig contains the Azbridge config to use on the remote connection
	ClientAzbridgeConfig *string `json:"clientAzbridgeConfig,omitempty"`
	// ErrorState is used to manage back-off retries for errors
	ErrorState *retry.ErrorState `json:"errorState,omitempty"`
}

// ServiceBridgeState represents the state of the ServiceBridge
type ServiceBridgeState string

const (
	// ServiceBridgeStatePending indicates that the service bridge is being initialized
	ServiceBridgeStatePending ServiceBridgeState = "Pending"
	// ServiceBridgeStateReady indicates that the service bridge is ready to connect to from the client
	ServiceBridgeStateReady ServiceBridgeState = "Ready"
	// ServiceBridgeStateError indicates that an error has occurred - check the events for more details
	ServiceBridgeStateError ServiceBridgeState = "Error"
)

// +kubebuilder:object:root=true
// The subresource line below is the key to updating the status without hitting "the server could not find the requested resource" - see https://github.com/kubernetes-sigs/kubebuilder/blob/a06ec9adbe2f3f4388399697f4cc30ed35fef2dd/docs/book/src/reference/generating-crd.md#status

// ServiceBridge is the Schema for the servicebridges API
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
type ServiceBridge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceBridgeSpec   `json:"spec,omitempty"`
	Status ServiceBridgeStatus `json:"status,omitempty"`
}

var _ retry.ObjectWithErrorState = &ServiceBridge{}

// GetErrorState implements ObjectWithErrorState
func (s *ServiceBridge) GetErrorState() *retry.ErrorState {
	return s.Status.ErrorState
}

// SetErrorState implements ObjectWithErrorState
func (s *ServiceBridge) SetErrorState(errorState *retry.ErrorState) {
	s.Status.ErrorState = errorState
}

// DeepCopyObjectWithErrorState implements ObjectWithErrorState
func (s *ServiceBridge) DeepCopyObjectWithErrorState() retry.ObjectWithErrorState {
	if c := s.DeepCopy(); c != nil {
		return c
	}
	return nil
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
