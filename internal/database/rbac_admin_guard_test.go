package database

import (
	"reflect"
	"testing"
)

func TestBuiltinAdminRejectsLossOfAccessAtomically(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled *bool
		roles   *[]string
	}{
		{name: "empty roles", roles: roleList()},
		{name: "operator only", roles: roleList(RBACSystemRoleOperator)},
		{name: "blank role", roles: roleList(" ")},
		{name: "disable", enabled: boolValue(false)},
		{name: "disable while retaining admin", enabled: boolValue(false), roles: roleList(RBACSystemRoleAdmin)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newRBACTestDB(t)
			if err := db.BootstrapRBAC("original-hash", map[string]string{"rbac:write": "manage", "auth:self": "self"}); err != nil {
				t.Fatal(err)
			}
			before, err := db.GetRBACUserByID("admin")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateRBACUser("admin", "must not persist", tc.enabled, tc.roles); err == nil {
				t.Fatal("destructive admin update succeeded")
			}
			after, err := db.GetRBACUserByID("admin")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected update changed account: before=%#v after=%#v", before, after)
			}
			access, err := db.ResolveRBACAccess("admin")
			if err != nil {
				t.Fatal(err)
			}
			if len(access.Roles) != 1 || access.Roles[0].ID != RBACSystemRoleAdmin || !access.Permissions["rbac:write"] || access.Scope != RBACScopeAll {
				t.Fatalf("admin access changed: %#v", access)
			}
		})
	}
}

func TestBuiltinAdminAllowsSafeMaintenanceAndMemberChanges(t *testing.T) {
	db := newRBACTestDB(t)
	if err := db.BootstrapRBAC("hash", map[string]string{"rbac:write": "manage"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateRBACUser("admin", "Renamed", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateRBACUser("admin", "Renamed", boolValue(true), roleList(" admin ", RBACSystemRoleViewer)); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateRBACUserPassword("admin", "new-hash"); err != nil {
		t.Fatal(err)
	}
	admin, err := db.GetRBACUserByID("admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.DisplayName != "Renamed" || admin.PasswordHash != "new-hash" || !admin.Enabled {
		t.Fatalf("safe maintenance failed: %#v", admin)
	}
	member, err := db.CreateRBACUser("operator", "Operator", "hash", true, []string{RBACSystemRoleAdmin, RBACSystemRoleOperator})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateRBACUser(member.ID, "Member", boolValue(false), roleList()); err != nil {
		t.Fatal(err)
	}
	access, err := db.ResolveRBACAccess(member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if access.User.Enabled || len(access.Roles) != 0 {
		t.Fatalf("ordinary member changes blocked: %#v", access)
	}
	if err := db.DeleteRBACUser("admin"); err == nil {
		t.Fatal("deleted builtin admin")
	}
}

func roleList(roles ...string) *[]string { return &roles }
func boolValue(value bool) *bool         { return &value }
