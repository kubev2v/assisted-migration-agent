package vmware

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// ValidateUserPrivilegesOnEntity checks whether the specified user has all the required privileges
// on a given vSphere entity (e.g., VM, folder, datacenter).
//
// Parameters:
//   - ctx: the context for the API request.
//   - ref: the ManagedObjectReference of the entity to check privileges on.
//   - requiredPrivileges: a list of privilege names that the user must have.
//   - userName: the name of the user to validate.
//
// Returns an error if:
//   - fetching the user's privileges fails,
//   - no privileges are returned for the user,
//   - or the user is missing any of the required privileges.
func (m *VMManager) ValidateUserPrivilegesOnEntity(ctx context.Context, ref types.ManagedObjectReference, requiredPrivileges []string, userName string) error {
	authManager := object.NewAuthorizationManager(m.gc.Client)

	results, err := authManager.FetchUserPrivilegeOnEntities(ctx, []types.ManagedObjectReference{ref}, userName)
	if err != nil {
		return fmt.Errorf("failed to fetch user privileges: %w", err)
	}

	if len(results) == 0 {
		return fmt.Errorf("no privileges returned for user %s", userName)
	}

	grantedMap := make(map[string]bool)
	for _, p := range results[0].Privileges {
		grantedMap[p] = true
	}

	var missing []string
	for _, req := range requiredPrivileges {
		if !grantedMap[req] {
			missing = append(missing, req)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("user %s is missing required privileges: %v", userName, missing)
	}

	return nil
}
