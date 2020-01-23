package v1alpha1

// AccountStateStatus defines the various status an Account CR can have
type AccountState string

const (
	// REQUESTED const for Requested status
	REQUESTED AccountState = "Requested"
	// CLAIMED const for Claimed status
	CLAIMED AccountState = "Claimed"
	// TRANSFERING const for Transfering status
	TRANSFERING AccountState = "Transfering"
	// TRANSFERED const for Transfering status
	TRANSFERED AccountState = "Transfered"
	// DELETING const for Deleting status
	DELETING AccountState = "Deleting"
	// PENDINGVERIFICATION const for Pending Verification status
	PENDINGVERIFICATION AccountState = "PendingVerification"

	// CREATING is set when an Account is being created
	CREATING AccountState = "Creating"
	// READY is set when an Account creation is ready
	READY AccountState = "Ready"
	// FAILED is set when account creation has failed
	FAILED AccountState = "Failed"
	// PENDING is set when account creation is pending
	PENDING AccountState = "Pending"
	// REUSED is set when account is reused
	REUSED AccountState = "Reused"
)
