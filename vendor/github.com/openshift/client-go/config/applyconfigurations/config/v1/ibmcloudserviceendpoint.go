// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

// IBMCloudServiceEndpointApplyConfiguration represents an declarative configuration of the IBMCloudServiceEndpoint type for use
// with apply.
type IBMCloudServiceEndpointApplyConfiguration struct {
	Name *string `json:"name,omitempty"`
	URL  *string `json:"url,omitempty"`
}

// IBMCloudServiceEndpointApplyConfiguration constructs an declarative configuration of the IBMCloudServiceEndpoint type for use with
// apply.
func IBMCloudServiceEndpoint() *IBMCloudServiceEndpointApplyConfiguration {
	return &IBMCloudServiceEndpointApplyConfiguration{}
}

// WithName sets the Name field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Name field is set to the value of the last call.
func (b *IBMCloudServiceEndpointApplyConfiguration) WithName(value string) *IBMCloudServiceEndpointApplyConfiguration {
	b.Name = &value
	return b
}

// WithURL sets the URL field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the URL field is set to the value of the last call.
func (b *IBMCloudServiceEndpointApplyConfiguration) WithURL(value string) *IBMCloudServiceEndpointApplyConfiguration {
	b.URL = &value
	return b
}
