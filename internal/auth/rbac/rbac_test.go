package rbac

import (
	"testing"
)

func TestRBAC_PermissionMatrix_OwnerVsReadOnly(t *testing.T) {
	// 1. Owner checks
	if !HasPermission(RoleOwner, PermOrgManage) {
		t.Errorf("Owner must have PermOrgManage")
	}
	if !HasPermission(RoleOwner, PermBillingManage) {
		t.Errorf("Owner must have PermBillingManage")
	}
	if !HasPermission(RoleOwner, PermKMSManage) {
		t.Errorf("Owner must have PermKMSManage")
	}
	if !HasPermission(RoleOwner, PermModelInvoke) {
		t.Errorf("Owner must have PermModelInvoke")
	}
	if !CanInvokeModel(RoleOwner) {
		t.Errorf("Owner must be able to invoke models")
	}
	if !CanManageKey(RoleOwner, "user-99", "user-1") {
		t.Errorf("Owner must be able to manage any key")
	}

	// 2. ReadOnly / Auditor checks
	readOnlyRoles := []Role{RoleReadOnly, RoleAuditor}
	for _, r := range readOnlyRoles {
		if !HasPermission(r, PermAuditRead) {
			t.Errorf("%s must have PermAuditRead", r)
		}
		if !HasPermission(r, PermConfigRead) {
			t.Errorf("%s must have PermConfigRead", r)
		}
		if !HasPermission(r, PermSpendView) {
			t.Errorf("%s must have PermSpendView", r)
		}

		// Forbidden capabilities
		if HasPermission(r, PermModelInvoke) {
			t.Errorf("%s must NOT have PermModelInvoke", r)
		}
		if CanInvokeModel(r) {
			t.Errorf("%s must NOT be able to invoke models", r)
		}
		if HasPermission(r, PermKeysManageAll) {
			t.Errorf("%s must NOT have PermKeysManageAll", r)
		}
		if HasPermission(r, PermKeysManageOwn) {
			t.Errorf("%s must NOT have PermKeysManageOwn", r)
		}
		if HasPermission(r, PermKMSManage) {
			t.Errorf("%s must NOT have PermKMSManage", r)
		}
		if CanManageKey(r, "user-self", "user-self") {
			t.Errorf("%s must NOT be able to manage keys", r)
		}
	}

	// 3. Admin checks
	if !HasPermission(RoleAdmin, PermKeysManageAll) {
		t.Errorf("Admin must have PermKeysManageAll")
	}
	if HasPermission(RoleAdmin, PermKMSManage) {
		t.Errorf("Admin must NOT have PermKMSManage (reserved for Owner)")
	}
	if HasPermission(RoleAdmin, PermOrgManage) {
		t.Errorf("Admin must NOT have PermOrgManage (reserved for Owner)")
	}

	// 4. Engineer checks
	if !CanInvokeModel(RoleEngineer) {
		t.Errorf("Engineer must be able to invoke models")
	}
	if !CanManageKey(RoleEngineer, "user-alice", "user-alice") {
		t.Errorf("Engineer must be able to manage own key")
	}
	if CanManageKey(RoleEngineer, "user-bob", "user-alice") {
		t.Errorf("Engineer must NOT be able to manage someone else's key")
	}
}
