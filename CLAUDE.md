I've analyzed the AWS Account Operator codebase and created a comprehensive CLAUDE.md file. The analysis covered:

**Key Architecture Insights:**
- Kubernetes operator managing AWS account lifecycle for OpenShift clusters
- 5 main Custom Resources: Account, AccountPool, AccountClaim, AWSFederatedRole, AWSFederatedAccountAccess
- Controllers organized by resource type in `controllers/` directory
- Uses AWS SDK for account management and IAM operations

**Development Commands Identified:**
- Build: `make go-build`
- Test: `make test-all`, `make test`, `make test-integration`
- Lint: `make lint`, `make go-check`
- Local development: `make deploy-local`, `make deploy-cluster`
- Code generation: `make generate`, `make op-generate`

**Special Features:**
- Support for multiple account types (standard, CCS, STS, Hypershift)
- FIPS-enabled by default
- Development mode that skips AWS support case creation
- Comprehensive integration testing framework
- Uses OpenShift boilerplate conventions

The CLAUDE.md file provides future Claude Code instances with essential information about the project structure, common development workflows, testing procedures, and key architectural patterns needed to work effectively in this repository.
