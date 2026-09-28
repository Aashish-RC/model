# Specification: RBAC, SSO & SCIM 2.0 Directory Sync

**Status:** Draft (Wave 3)  
**Layer:** Identity & Access Management (`internal/auth/rbac`, `internal/auth/scim`)  
**Design Reference Mode:** `OWN` (RFC 7643 / RFC 7644 Clean-Room standards)  

---

## 1. Purpose & Overview

Enterprise deployments require role-based permission boundaries and automatic user/group synchronization from corporate identity providers (such as Okta, Microsoft Entra ID, PingIdentity, or Google Workspace) via the standard SCIM 2.0 protocol.

---

## 2. Role-Based Access Control (RBAC)

### 2.1 Role Hierarchy & Permissions
1. **Owner:**
   - Full administrative control: manage organizations, billing, KMS master keys, audit logs, and users.
2. **Admin:**
   - Manage virtual keys, routing rules, model catalogs, and view spend reports.
3. **Engineer:**
   - Create and revoke own virtual keys; invoke models; view personal usage.
4. **ReadOnly / Auditor:**
   - Inspect configuration, metrics, spend reports, and cryptographic audit logs; cannot invoke models or edit keys.

---

## 3. SCIM 2.0 Provisioning Engine

Implements the RFC 7644 REST protocol:
- **`GET /scim/v2/Users`**: List users with pagination (`startIndex`, `count`).
- **`POST /scim/v2/Users`**: Provision a new user and assign default role.
- **`GET /scim/v2/Users/{id}`**: Retrieve user profile.
- **`PATCH /scim/v2/Users/{id}`** or **`PUT`**: Update user metadata, activate, or suspend.
- **`DELETE /scim/v2/Users/{id}`**: Soft-delete or de-provision user, automatically disabling associated virtual keys.
- **`GET /scim/v2/Groups`** & **`POST /scim/v2/Groups`**: Synchronize team/group membership.

---

## 4. Security & Authentication

- SCIM endpoints are secured via bearer token authentication (`Authorization: Bearer <scim-api-token>`).
- All provisioning events emit cryptographically linked audit log entries.

---

## 5. Acceptance Tests

1. `TestRBAC_PermissionMatrix_OwnerVsReadOnly`
2. `TestSCIM_CreateUser_AssignsRoleAndGeneratesID`
3. `TestSCIM_DeactivateUser_RevokesActiveVirtualKeys`
4. `TestSCIM_GroupSync_MapsToGatewayTeams`
5. `TestSCIM_UnauthorizedRequest_Rejects401`
