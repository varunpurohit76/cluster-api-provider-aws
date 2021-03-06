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

package bootstrap

import (
	"fmt"

	"github.com/awslabs/goformation/v4/cloudformation"
	cfn_iam "github.com/awslabs/goformation/v4/cloudformation/iam"

	bootstrapv1 "sigs.k8s.io/cluster-api-provider-aws/cmd/clusterawsadm/api/bootstrap/v1alpha1"
	iamv1 "sigs.k8s.io/cluster-api-provider-aws/cmd/clusterawsadm/api/iam/v1alpha1"
	"sigs.k8s.io/cluster-api-provider-aws/cmd/clusterawsadm/converters"
	infrav1exp "sigs.k8s.io/cluster-api-provider-aws/exp/api/v1alpha3"
)

const (
	AWSIAMGroupBootstrapper                      = "AWSIAMGroupBootstrapper"
	AWSIAMInstanceProfileControllers             = "AWSIAMInstanceProfileControllers"
	AWSIAMInstanceProfileControlPlane            = "AWSIAMInstanceProfileControlPlane"
	AWSIAMInstanceProfileNodes                   = "AWSIAMInstanceProfileNodes"
	AWSIAMRoleControllers                        = "AWSIAMRoleControllers"
	AWSIAMRoleControlPlane                       = "AWSIAMRoleControlPlane"
	AWSIAMRoleNodes                              = "AWSIAMRoleNodes"
	AWSIAMRoleEKSControlPlane                    = "AWSIAMRoleEKSControlPlane"
	AWSIAMUserBootstrapper                       = "AWSIAMUserBootstrapper"
	ControllersPolicy                 PolicyName = "AWSIAMManagedPolicyControllers"
	ControlPlanePolicy                PolicyName = "AWSIAMManagedPolicyCloudProviderControlPlane"
	NodePolicy                        PolicyName = "AWSIAMManagedPolicyCloudProviderNodes"
	CSIPolicy                         PolicyName = "AWSEBSCSIPolicyController"
)

type Template struct {
	Spec *bootstrapv1.AWSIAMConfigurationSpec
}

func NewTemplate() Template {
	conf := bootstrapv1.NewAWSIAMConfiguration()
	return Template{
		Spec: &conf.Spec,
	}
}

// NewManagedName creates an IAM acceptable name prefixed with this Cluster API
// implementation's prefix.
func (t Template) NewManagedName(name string) string {
	return fmt.Sprintf("%s%s%s", t.Spec.NamePrefix, name, *t.Spec.NameSuffix)
}

// Template is an AWS CloudFormation template to bootstrap
// IAM policies, users and roles for use by Cluster API Provider AWS
func (t Template) RenderCloudFormation() *cloudformation.Template {
	template := cloudformation.NewTemplate()

	if t.Spec.BootstrapUser.Enable {
		template.Resources[AWSIAMUserBootstrapper] = &cfn_iam.User{
			UserName:          t.Spec.BootstrapUser.UserName,
			Groups:            t.bootstrapUserGroups(),
			ManagedPolicyArns: t.Spec.ControlPlane.ExtraPolicyAttachments,
			Policies:          t.bootstrapUserPolicy(),
			Tags:              converters.MapToCloudFormationTags(t.Spec.BootstrapUser.Tags),
		}

		template.Resources[AWSIAMGroupBootstrapper] = &cfn_iam.Group{
			GroupName: t.Spec.BootstrapUser.GroupName,
		}
	}

	template.Resources[string(ControllersPolicy)] = &cfn_iam.ManagedPolicy{
		ManagedPolicyName: t.NewManagedName("controllers"),
		Description:       `For the Kubernetes Cluster API Provider AWS Controllers`,
		PolicyDocument:    t.controllersPolicy(),
		Groups:            t.controllersPolicyGroups(),
		Roles:             t.controllersPolicyRoleAttachments(),
	}

	if !t.Spec.ControlPlane.DisableCloudProviderPolicy {
		template.Resources[string(ControlPlanePolicy)] = &cfn_iam.ManagedPolicy{
			ManagedPolicyName: t.NewManagedName("control-plane"),
			Description:       `For the Kubernetes Cloud Provider AWS Control Plane`,
			PolicyDocument:    t.cloudProviderControlPlaneAwsPolicy(),
			Roles:             t.cloudProviderControlPlaneAwsRoles(),
		}
	}

	if !t.Spec.Nodes.DisableCloudProviderPolicy {
		template.Resources[string(NodePolicy)] = &cfn_iam.ManagedPolicy{
			ManagedPolicyName: t.NewManagedName("nodes"),
			Description:       `For the Kubernetes Cloud Provider AWS nodes`,
			PolicyDocument:    t.nodePolicy(),
			Roles:             t.cloudProviderNodeAwsRoles(),
		}
	}

	if t.Spec.ControlPlane.EnableCSIPolicy {
		template.Resources[string(CSIPolicy)] = &cfn_iam.ManagedPolicy{
			ManagedPolicyName: t.NewManagedName("csi"),
			Description:       `For the AWS EBS CSI Driver for Kubernetes`,
			PolicyDocument:    t.csiControllerPolicy(),
			Roles:             t.csiControlPlaneAwsRoles(),
		}
	}

	template.Resources[AWSIAMRoleControlPlane] = &cfn_iam.Role{
		RoleName:                 t.NewManagedName("control-plane"),
		AssumeRolePolicyDocument: t.controlPlaneTrustPolicy(),
		ManagedPolicyArns:        t.Spec.ControlPlane.ExtraPolicyAttachments,
		Policies:                 t.controlPlanePolicies(),
		Tags:                     converters.MapToCloudFormationTags(t.Spec.ControlPlane.Tags),
	}

	template.Resources[AWSIAMRoleControllers] = &cfn_iam.Role{
		RoleName:                 t.NewManagedName("controllers"),
		AssumeRolePolicyDocument: t.controllersTrustPolicy(),
		Tags:                     converters.MapToCloudFormationTags(t.Spec.ClusterAPIControllers.Tags),
	}

	template.Resources[AWSIAMRoleNodes] = &cfn_iam.Role{
		RoleName:                 t.NewManagedName("nodes"),
		AssumeRolePolicyDocument: t.nodeTrustPolicy(),
		ManagedPolicyArns:        t.Spec.Nodes.ExtraPolicyAttachments,
		Policies:                 t.nodePolicies(),
		Tags:                     converters.MapToCloudFormationTags(t.Spec.Nodes.Tags),
	}

	template.Resources[AWSIAMInstanceProfileControlPlane] = &cfn_iam.InstanceProfile{
		InstanceProfileName: t.NewManagedName("control-plane"),
		Roles: []string{
			cloudformation.Ref(AWSIAMRoleControlPlane),
		},
	}

	template.Resources[AWSIAMInstanceProfileControllers] = &cfn_iam.InstanceProfile{
		InstanceProfileName: t.NewManagedName("controllers"),
		Roles: []string{
			cloudformation.Ref(AWSIAMRoleControllers),
		},
	}

	template.Resources[AWSIAMInstanceProfileNodes] = &cfn_iam.InstanceProfile{
		InstanceProfileName: t.NewManagedName("nodes"),
		Roles: []string{
			cloudformation.Ref(AWSIAMRoleNodes),
		},
	}

	if !t.Spec.ManagedControlPlane.Disable {
		template.Resources[AWSIAMRoleEKSControlPlane] = &cfn_iam.Role{
			RoleName:                 infrav1exp.DefaultEKSControlPlaneRole,
			AssumeRolePolicyDocument: eksAssumeRolePolicy(),
			ManagedPolicyArns:        t.eksControlPlanePolicies(),
			Tags:                     converters.MapToCloudFormationTags(t.Spec.ManagedControlPlane.Tags),
		}
	}

	return template
}

func ec2AssumeRolePolicy() *iamv1.PolicyDocument {
	return assumeRolePolicy("ec2.amazonaws.com")
}

func assumeRolePolicy(principalID string) *iamv1.PolicyDocument {
	return &iamv1.PolicyDocument{
		Version: iamv1.CurrentVersion,
		Statement: []iamv1.StatementEntry{
			{
				Effect:    iamv1.EffectAllow,
				Principal: iamv1.Principals{iamv1.PrincipalService: iamv1.PrincipalID{principalID}},
				Action:    iamv1.Actions{"sts:AssumeRole"},
			},
		},
	}
}
