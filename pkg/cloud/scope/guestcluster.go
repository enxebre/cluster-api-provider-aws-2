/*
Copyright 2020 The Kubernetes Authors.

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

package scope

import (
	"fmt"

	awsclient "github.com/aws/aws-sdk-go/aws/client"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/klogr"
	infrav1 "sigs.k8s.io/cluster-api-provider-aws/api/v1alpha3"
	"sigs.k8s.io/cluster-api-provider-aws/pkg/cloud"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GuestClusterScopeParams defines the input parameters used to create a new Scope.
type GuestClusterScopeParams struct {
	Client         client.Client
	Logger         logr.Logger
	Cluster        *clusterv1.Cluster
	GuestCluster   *unstructured.Unstructured
	ControllerName string
	Endpoints      []ServiceEndpoint
	Session        awsclient.ConfigProvider
}

// NewGuestClusterScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewGuestClusterScope(params GuestClusterScopeParams) (*GuestClusterScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.GuestCluster == nil {
		return nil, errors.New("failed to generate new scope from nil GuestCluster")
	}

	if params.Logger == nil {
		params.Logger = klogr.New()
	}

	region, found, err := unstructured.NestedString(params.GuestCluster.Object, "spec", "region")
	if err != nil || !found {
		return nil, fmt.Errorf("error getting region: %w", err)
	}
	session, err := sessionForRegion(region, params.Endpoints)
	if err != nil {
		return nil, errors.Errorf("failed to create aws session: %v", err)
	}

	helper, err := patch.NewHelper(params.GuestCluster, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}
	return &GuestClusterScope{
		Logger:         params.Logger,
		client:         params.Client,
		Cluster:        params.Cluster,
		GuestCluster:   &GuestClusterObject{params.GuestCluster},
		patchHelper:    helper,
		session:        session,
		controllerName: params.ControllerName,
	}, nil
}

// GuestClusterScope defines the basic context for an actuator to operate upon.
type GuestClusterScope struct {
	logr.Logger
	client      client.Client
	patchHelper *patch.Helper

	Cluster      *clusterv1.Cluster
	GuestCluster *GuestClusterObject

	session        awsclient.ConfigProvider
	controllerName string
}

// Network returns the cluster network object.
func (s *GuestClusterScope) Network() *infrav1.Network {
	return nil
}

// VPC returns the cluster VPC.
func (s *GuestClusterScope) VPC() *infrav1.VPCSpec {
	return &infrav1.VPCSpec{}
}

// Subnets returns the cluster subnets.
func (s *GuestClusterScope) Subnets() infrav1.Subnets {
	return nil
}

// SetSubnets updates the clusters subnets.
func (s *GuestClusterScope) SetSubnets(subnets infrav1.Subnets) {
}

// CNIIngressRules returns the CNI spec ingress rules.
func (s *GuestClusterScope) CNIIngressRules() infrav1.CNIIngressRules {
	return infrav1.CNIIngressRules{}
}

// SecurityGroups returns the cluster security groups as a map, it creates the map if empty.
func (s *GuestClusterScope) SecurityGroups() map[infrav1.SecurityGroupRole]infrav1.SecurityGroup {
	return nil
}

// Name returns the CAPI cluster name.
func (s *GuestClusterScope) Name() string {
	return s.Cluster.Name
}

// Namespace returns the cluster namespace.
func (s *GuestClusterScope) Namespace() string {
	return s.Cluster.Namespace
}

// Region returns the cluster region.
func (s *GuestClusterScope) Region() string {
	region, found, err := unstructured.NestedString(s.GuestCluster.Object, "spec", "region")
	if err != nil || !found {
		s.Error(err, "error getting region")
		return ""
	}
	return region
}

// KubernetesClusterName is the name of the Kubernetes cluster. For the cluster
// scope this is the same as the CAPI cluster name
func (s *GuestClusterScope) KubernetesClusterName() string {
	return s.Cluster.Name
}

// ControlPlaneLoadBalancer returns the AWSLoadBalancerSpec
func (s *GuestClusterScope) ControlPlaneLoadBalancer() *infrav1.AWSLoadBalancerSpec {
	return nil
}

// ControlPlaneLoadBalancerScheme returns the Classic ELB scheme (public or internal facing)
func (s *GuestClusterScope) ControlPlaneLoadBalancerScheme() infrav1.ClassicELBScheme {
	if s.ControlPlaneLoadBalancer() != nil && s.ControlPlaneLoadBalancer().Scheme != nil {
		return *s.ControlPlaneLoadBalancer().Scheme
	}
	return infrav1.ClassicELBSchemeInternetFacing
}

// ControlPlaneConfigMapName returns the name of the ConfigMap used to
// coordinate the bootstrapping of control plane nodes.
func (s *GuestClusterScope) ControlPlaneConfigMapName() string {
	return fmt.Sprintf("%s-controlplane", s.Cluster.UID)
}

// ListOptionsLabelSelector returns a ListOptions with a label selector for clusterName.
func (s *GuestClusterScope) ListOptionsLabelSelector() client.ListOption {
	return client.MatchingLabels(map[string]string{
		clusterv1.ClusterLabelName: s.Cluster.Name,
	})
}

// PatchObject persists the cluster configuration and status.
func (s *GuestClusterScope) PatchObject() error {
	return nil
	// Always update the readyCondition by summarizing the state of other conditions.
	// A step counter is added to represent progress during the provisioning process (instead we are hiding during the deletion process).
	//applicableConditions := []clusterv1.ConditionType{
	//	infrav1.VpcReadyCondition,
	//	infrav1.SubnetsReadyCondition,
	//	infrav1.ClusterSecurityGroupsReadyCondition,
	//	infrav1.LoadBalancerReadyCondition,
	//}
	//
	//if s.VPC().IsManaged(s.Name()) {
	//	applicableConditions = append(applicableConditions,
	//		infrav1.InternetGatewayReadyCondition,
	//		infrav1.NatGatewaysReadyCondition,
	//		infrav1.RouteTablesReadyCondition)
	//
	//	if s.GuestCluster.Spec.Bastion.Enabled {
	//		applicableConditions = append(applicableConditions, infrav1.BastionHostReadyCondition)
	//	}
	//}
	//
	//conditions.SetSummary(s.GuestCluster,
	//	conditions.WithConditions(applicableConditions...),
	//	conditions.WithStepCounterIf(s.GuestCluster.ObjectMeta.DeletionTimestamp.IsZero()),
	//	conditions.WithStepCounter(),
	//)
	//
	//return s.patchHelper.Patch(
	//	context.TODO(),
	//	s.GuestCluster,
	//	patch.WithOwnedConditions{Conditions: []clusterv1.ConditionType{
	//		clusterv1.ReadyCondition,
	//		infrav1.VpcReadyCondition,
	//		infrav1.SubnetsReadyCondition,
	//		infrav1.InternetGatewayReadyCondition,
	//		infrav1.NatGatewaysReadyCondition,
	//		infrav1.RouteTablesReadyCondition,
	//		infrav1.ClusterSecurityGroupsReadyCondition,
	//		infrav1.BastionHostReadyCondition,
	//		infrav1.LoadBalancerReadyCondition,
	//	}})
}

// Close closes the current scope persisting the cluster configuration and status.
func (s *GuestClusterScope) Close() error {
	return s.PatchObject()
}

// AdditionalTags returns AdditionalTags from the scope's GuestCluster. The returned value will never be nil.
func (s *GuestClusterScope) AdditionalTags() infrav1.Tags {
	return nil
}

// APIServerPort returns the APIServerPort to use when creating the load balancer.
func (s *GuestClusterScope) APIServerPort() int32 {
	if s.Cluster.Spec.ClusterNetwork != nil && s.Cluster.Spec.ClusterNetwork.APIServerPort != nil {
		return *s.Cluster.Spec.ClusterNetwork.APIServerPort
	}
	return 6443
}

// SetFailureDomain sets the infrastructure provider failure domain key to the spec given as input.
func (s *GuestClusterScope) SetFailureDomain(id string, spec clusterv1.FailureDomainSpec) {
}

type GuestClusterObject struct {
	*unstructured.Unstructured
}

// InfraCluster returns the AWS infrastructure cluster or control plane object.
func (s *GuestClusterScope) InfraCluster() cloud.ClusterObject {
	return s.GuestCluster
}

func (r *GuestClusterObject) GetConditions() clusterv1.Conditions {
	return nil
}

func (r *GuestClusterObject) SetConditions(conditions clusterv1.Conditions) {
}

// Session returns the AWS SDK session. Used for creating clients
func (s *GuestClusterScope) Session() awsclient.ConfigProvider {
	return s.session
}

// Bastion returns the bastion details.
func (s *GuestClusterScope) Bastion() *infrav1.Bastion {
	return nil
}

// SetBastionInstance sets the bastion instance in the status of the cluster.
func (s *GuestClusterScope) SetBastionInstance(instance *infrav1.Instance) {
	//s.GuestCluster.Status.Bastion = instance
}

// SSHKeyName returns the SSH key name to use for instances.
func (s *GuestClusterScope) SSHKeyName() *string {
	//return s.GuestCluster.Spec.SSHKeyName
	return nil
}

// ControllerName returns the name of the controller that
// created the GuestClusterScope.
func (s *GuestClusterScope) ControllerName() string {
	return s.controllerName
}

// ImageLookupFormat returns the format string to use when looking up AMIs
func (s *GuestClusterScope) ImageLookupFormat() string {
	return ""
}

// ImageLookupOrg returns the organization name to use when looking up AMIs
func (s *GuestClusterScope) ImageLookupOrg() string {
	return ""
}

// ImageLookupBaseOS returns the base operating system name to use when looking up AMIs
func (s *GuestClusterScope) ImageLookupBaseOS() string {
	return ""
}
