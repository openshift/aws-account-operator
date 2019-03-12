package controller

import (
	"github.com/openshift/aws-account-operator/pkg/controller/accountpool"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, accountpool.Add)
}
