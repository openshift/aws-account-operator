// +build !ignore_autogenerated

// Code generated by openapi-gen. DO NOT EDIT.

// This file was autogenerated by openapi-gen. Do not edit it manually!

package v1alpha1

import (
	spec "github.com/go-openapi/spec"
	common "k8s.io/kube-openapi/pkg/common"
)

func GetOpenAPIDefinitions(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	return map[string]common.OpenAPIDefinition{
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccess":       schema_pkg_apis_aws_v1alpha1_AWSFederatedAccountAccess(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccessSpec":   schema_pkg_apis_aws_v1alpha1_AWSFederatedAccountAccessSpec(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccessStatus": schema_pkg_apis_aws_v1alpha1_AWSFederatedAccountAccessStatus(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRole":                schema_pkg_apis_aws_v1alpha1_AWSFederatedRole(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleSpec":            schema_pkg_apis_aws_v1alpha1_AWSFederatedRoleSpec(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleStatus":          schema_pkg_apis_aws_v1alpha1_AWSFederatedRoleStatus(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.Account":                         schema_pkg_apis_aws_v1alpha1_Account(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaim":                    schema_pkg_apis_aws_v1alpha1_AccountClaim(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaimSpec":                schema_pkg_apis_aws_v1alpha1_AccountClaimSpec(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaimStatus":              schema_pkg_apis_aws_v1alpha1_AccountClaimStatus(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountPool":                     schema_pkg_apis_aws_v1alpha1_AccountPool(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountPoolSpec":                 schema_pkg_apis_aws_v1alpha1_AccountPoolSpec(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountPoolStatus":               schema_pkg_apis_aws_v1alpha1_AccountPoolStatus(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountSpec":                     schema_pkg_apis_aws_v1alpha1_AccountSpec(ref),
		"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountStatus":                   schema_pkg_apis_aws_v1alpha1_AccountStatus(ref),
	}
}

func schema_pkg_apis_aws_v1alpha1_AWSFederatedAccountAccess(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AWSFederatedAccountAccess is the Schema for the awsfederatedaccountaccesses API",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"apiVersion": {
						SchemaProps: spec.SchemaProps{
							Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"metadata": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"),
						},
					},
					"spec": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccessSpec"),
						},
					},
					"status": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccessStatus"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccessSpec", "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccessStatus", "k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AWSFederatedAccountAccessSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AWSFederatedAccountAccessSpec defines the desired state of AWSFederatedAccountAccess",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"externalCustomerAWSIAMARN": {
						SchemaProps: spec.SchemaProps{
							Description: "ExternalCustomerAWSARN holds the external AWS IAM ARN",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"awsCustomerCredentialSecret": {
						SchemaProps: spec.SchemaProps{
							Description: "AWSCustomerCredentialSecret holds the credentials to the cluster account where the role wil be created",
							Ref:         ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSSecretReference"),
						},
					},
					"awsFederatedRole": {
						SchemaProps: spec.SchemaProps{
							Description: "FederatedRoleName must be the name of a federatedrole cr that currently exists",
							Ref:         ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleRef"),
						},
					},
				},
				Required: []string{"externalCustomerAWSIAMARN", "awsCustomerCredentialSecret", "awsFederatedRole"},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleRef", "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSSecretReference"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AWSFederatedAccountAccessStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AWSFederatedAccountAccessStatus defines the observed state of AWSFederatedAccountAccess",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"conditions": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-map-keys": []interface{}{
									"type",
								},
								"x-kubernetes-list-type": "map",
							},
						},
						SchemaProps: spec.SchemaProps{
							Type: []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccessCondition"),
									},
								},
							},
						},
					},
					"state": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"consoleURL": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
				},
				Required: []string{"conditions", "state"},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedAccountAccessCondition"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AWSFederatedRole(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AWSFederatedRole is the Schema for the awsfederatedroles API",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"apiVersion": {
						SchemaProps: spec.SchemaProps{
							Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"metadata": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"),
						},
					},
					"spec": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleSpec"),
						},
					},
					"status": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleStatus"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleSpec", "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleStatus", "k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AWSFederatedRoleSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AWSFederatedRoleSpec defines the desired state of AWSFederatedRole",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"roleDisplayName": {
						SchemaProps: spec.SchemaProps{
							Description: "RoleDisplayName is a user friendly display name for the OCM user interface",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"roleDescription": {
						SchemaProps: spec.SchemaProps{
							Description: "RoleDescription is a user friendly description of the role, this discription will be displayed in the OCM user interface",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"awsCustomPolicy": {
						SchemaProps: spec.SchemaProps{
							Description: "AWSCustomPolicy is the defenition of a custom aws permission policy that will be associated with this role",
							Ref:         ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSCustomPolicy"),
						},
					},
					"awsManagedPolicies": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-type": "atomic",
							},
						},
						SchemaProps: spec.SchemaProps{
							Description: "AWSManagedPolicies is a list of amazong managed policies that exist in aws",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Type:   []string{"string"},
										Format: "",
									},
								},
							},
						},
					},
				},
				Required: []string{"roleDisplayName", "roleDescription"},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSCustomPolicy"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AWSFederatedRoleStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AWSFederatedRoleStatus defines the observed state of AWSFederatedRole",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"state": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"conditions": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-map-keys": []interface{}{
									"type",
								},
								"x-kubernetes-list-type": "map",
							},
						},
						SchemaProps: spec.SchemaProps{
							Type: []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleCondition"),
									},
								},
							},
						},
					},
				},
				Required: []string{"state", "conditions"},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AWSFederatedRoleCondition"},
	}
}

func schema_pkg_apis_aws_v1alpha1_Account(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "Account is the Schema for the accounts API",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"apiVersion": {
						SchemaProps: spec.SchemaProps{
							Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"metadata": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"),
						},
					},
					"spec": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountSpec"),
						},
					},
					"status": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountStatus"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountSpec", "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountStatus", "k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AccountClaim(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AccountClaim is the Schema for the accountclaims API",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"apiVersion": {
						SchemaProps: spec.SchemaProps{
							Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"metadata": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"),
						},
					},
					"spec": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaimSpec"),
						},
					},
					"status": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaimStatus"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaimSpec", "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaimStatus", "k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AccountClaimSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AccountClaimSpec defines the desired state of AccountClaim",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"legalEntity": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.LegalEntity"),
						},
					},
					"awsCredentialSecret": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.SecretRef"),
						},
					},
					"aws": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.Aws"),
						},
					},
					"accountLink": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"accountOU": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"byoc": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"boolean"},
							Format: "",
						},
					},
					"byocSecretRef": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.SecretRef"),
						},
					},
					"byocAWSAccountID": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"manualSTSMode": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"boolean"},
							Format: "",
						},
					},
					"stsRoleARN": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"stsExternalID": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"supportRoleARN": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"customTags": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
				},
				Required: []string{"legalEntity", "awsCredentialSecret", "aws", "accountLink"},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.Aws", "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.LegalEntity", "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.SecretRef"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AccountClaimStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AccountClaimStatus defines the observed state of AccountClaim",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"conditions": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-map-keys": []interface{}{
									"type",
								},
								"x-kubernetes-list-type": "map",
							},
						},
						SchemaProps: spec.SchemaProps{
							Type: []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaimCondition"),
									},
								},
							},
						},
					},
					"state": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
				},
				Required: []string{"conditions", "state"},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountClaimCondition"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AccountPool(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AccountPool is the Schema for the accountpools API",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"apiVersion": {
						SchemaProps: spec.SchemaProps{
							Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"metadata": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"),
						},
					},
					"spec": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountPoolSpec"),
						},
					},
					"status": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountPoolStatus"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountPoolSpec", "github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountPoolStatus", "k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AccountPoolSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AccountPoolSpec defines the desired state of AccountPool",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"poolSize": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int32",
						},
					},
				},
				Required: []string{"poolSize"},
			},
		},
	}
}

func schema_pkg_apis_aws_v1alpha1_AccountPoolStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AccountPoolStatus defines the observed state of AccountPool",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"poolSize": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int32",
						},
					},
					"unclaimedAccounts": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int32",
						},
					},
					"claimedAccounts": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"integer"},
							Format: "int32",
						},
					},
				},
				Required: []string{"poolSize", "unclaimedAccounts", "claimedAccounts"},
			},
		},
	}
}

func schema_pkg_apis_aws_v1alpha1_AccountSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AccountSpec defines the desired state of Account",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"awsAccountID": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"iamUserSecret": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"byoc": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"boolean"},
							Format: "",
						},
					},
					"claimLink": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"claimLinkNamespace": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"legalEntity": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.LegalEntity"),
						},
					},
					"manualSTSMode": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"boolean"},
							Format: "",
						},
					},
				},
				Required: []string{"awsAccountID", "iamUserSecret"},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.LegalEntity"},
	}
}

func schema_pkg_apis_aws_v1alpha1_AccountStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "AccountStatus defines the observed state of Account",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"claimed": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"boolean"},
							Format: "",
						},
					},
					"supportCaseID": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"conditions": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-map-keys": []interface{}{
									"type",
								},
								"x-kubernetes-list-type": "map",
							},
						},
						SchemaProps: spec.SchemaProps{
							Type: []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountCondition"),
									},
								},
							},
						},
					},
					"state": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"rotateCredentials": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"boolean"},
							Format: "",
						},
					},
					"rotateConsoleCredentials": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"boolean"},
							Format: "",
						},
					},
					"reused": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"boolean"},
							Format: "",
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/openshift/aws-account-operator/pkg/apis/aws/v1alpha1.AccountCondition"},
	}
}
