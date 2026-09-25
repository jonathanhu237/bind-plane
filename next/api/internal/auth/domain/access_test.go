package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestPermissionCatalogIsStrictAndDeterministic(t *testing.T) {
	catalog, err := NewPermissionCatalog(
		PermissionDefinition{Key: PermissionRolesRead, Resource: "roles", Action: "read", LabelKey: "roles.read"},
		PermissionDefinition{Key: PermissionUsersRead, Resource: "users", Action: "read", LabelKey: "users.read"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Keys(); !reflect.DeepEqual(got, []PermissionKey{PermissionRolesRead, PermissionUsersRead}) {
		t.Fatalf("catalog keys = %#v", got)
	}
	permissions, err := catalog.Validate([]PermissionKey{PermissionUsersRead, PermissionUsersRead})
	if err != nil || !reflect.DeepEqual(permissions, []PermissionKey{PermissionUsersRead}) {
		t.Fatalf("Validate() = %#v, %v", permissions, err)
	}
	if _, err := catalog.Validate(nil); err == nil {
		t.Fatal("empty permission set accepted")
	} else {
		var validation *ValidationErrors
		if !errors.As(err, &validation) || validation.Items[0].Code != "empty_permissions" {
			t.Fatalf("empty permission error = %v", err)
		}
	}
	if _, err := catalog.Validate([]PermissionKey{"projects.write"}); err == nil {
		t.Fatal("unknown permission accepted")
	} else {
		var validation *ValidationErrors
		if !errors.As(err, &validation) || validation.Items[0].Code != "unknown_permission" {
			t.Fatalf("unknown permission error = %v", err)
		}
	}
	for _, definitions := range [][]PermissionDefinition{
		{{Key: "bad key", Resource: "x", Action: "read", LabelKey: "x.read"}},
		{{Key: PermissionUsersRead, Resource: "x", Action: "read", LabelKey: "x.read"}, {Key: PermissionUsersRead, Resource: "x", Action: "read", LabelKey: "x.read"}},
	} {
		if _, err := NewPermissionCatalog(definitions...); err == nil {
			t.Fatalf("invalid catalog accepted: %#v", definitions)
		}
	}
}

func TestPermissionCatalogKeepsReadWriteIndependentAndValidatesCombinations(t *testing.T) {
	catalog := DefaultPermissionCatalog()
	if _, err := catalog.ValidateFeatureSet([]PermissionKey{PermissionOnlineUsersWrite}); err != nil {
		t.Fatalf("online-users.write must remain independently grantable: %v", err)
	}
	permissions, err := catalog.Validate([]PermissionKey{PermissionInvitationsWrite})
	if err != nil || !reflect.DeepEqual(permissions, []PermissionKey{PermissionInvitationsWrite}) {
		t.Fatalf("Validate(invitations.write) = %#v, %v", permissions, err)
	}
	if dependencies := catalog.Dependencies(PermissionInvitationsWrite); len(dependencies) != 0 {
		t.Fatalf("Dependencies(invitations.write) = %#v, want empty", dependencies)
	}
	if _, err := catalog.ValidateFeatureSet([]PermissionKey{PermissionInvitationsWrite}); err == nil {
		t.Fatal("incomplete invitation combination accepted")
	} else {
		var validation *ValidationErrors
		if !errors.As(err, &validation) || validation.Items[0].Code != "incomplete_permission_combination" {
			t.Fatalf("incomplete combination error = %v", err)
		}
	}
	permissions, err = catalog.ValidateFeatureSet([]PermissionKey{PermissionInvitationsWrite, PermissionRolesRead})
	if err != nil || !reflect.DeepEqual(permissions, []PermissionKey{PermissionInvitationsWrite, PermissionRolesRead}) {
		t.Fatalf("complete invitation combination = %#v, %v", permissions, err)
	}
	if _, err := catalog.ValidateFeatureSet([]PermissionKey{PermissionInvitationsRead, PermissionInvitationsWrite}); err == nil {
		t.Fatal("incomplete invitation management combination accepted")
	}
}

func TestPrincipalPermissionsAreUnionedAndSuperAdminExpandsCatalog(t *testing.T) {
	catalog := DefaultPermissionCatalog()
	principal := Principal{Permissions: []PermissionKey{PermissionUsersRead, PermissionUsersRead, "future.write"}}
	if !principal.Has(PermissionUsersRead) || principal.Has(PermissionRolesRead) {
		t.Fatal("principal Has() did not apply default-deny semantics")
	}
	if got := principal.EffectivePermissions(catalog); !reflect.DeepEqual(got, []PermissionKey{PermissionUsersRead}) {
		t.Fatalf("effective permissions = %#v", got)
	}
	super := Principal{SuperAdmin: true}
	if got := super.EffectivePermissions(catalog); !reflect.DeepEqual(got, catalog.Keys()) {
		t.Fatalf("super-admin permissions = %#v, want %#v", got, catalog.Keys())
	}
}

func TestCanonicalRoleNameAndUUIDValidation(t *testing.T) {
	if got := CanonicalRoleName("  Read   Only "); got != "read only" {
		t.Fatalf("CanonicalRoleName() = %q", got)
	}
	if !IsCanonicalUUID("019535d9-3df7-79fb-b466-fa907fa17f9e") {
		t.Fatal("canonical UUID rejected")
	}
	for _, value := range []string{"019535D9-3df7-79fb-b466-fa907fa17f9e", "{019535d9-3df7-79fb-b466-fa907fa17f9e}", "not-a-uuid"} {
		if IsCanonicalUUID(value) {
			t.Fatalf("non-canonical UUID accepted: %q", value)
		}
	}
}
