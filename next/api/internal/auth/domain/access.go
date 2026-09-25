package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type PermissionKey string

type PermissionDefinition struct {
	Key          PermissionKey
	Resource     string
	Action       string
	LabelKey     string
	Description  string
	Dependencies []PermissionKey
}

type PermissionCombination struct {
	Key         string
	LabelKey    string
	Description string
	Permissions []PermissionKey
	// Trigger lists the permissions which activate this combination. Keeping
	// this separate from Permissions lets overlapping capabilities remain
	// independently grantable. For example, invitations.write can activate
	// invitation creation without also activating the full invitation
	// management combination, which additionally requires invitations.read.
	Trigger []PermissionKey
}

type PermissionCatalog struct {
	items            map[PermissionKey]PermissionDefinition
	combinations     []PermissionCombination
	combinationError error
}

var permissionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*\.[a-z][a-z0-9_-]*$`)

func NewPermissionCatalog(definitions ...PermissionDefinition) (PermissionCatalog, error) {
	items := make(map[PermissionKey]PermissionDefinition, len(definitions))
	for _, definition := range definitions {
		key := definition.Key
		if !permissionKeyPattern.MatchString(string(key)) || definition.Resource == "" || definition.Action == "" || definition.LabelKey == "" {
			return PermissionCatalog{}, fmt.Errorf("invalid permission definition %q", key)
		}
		if _, exists := items[key]; exists {
			return PermissionCatalog{}, fmt.Errorf("duplicate permission definition %q", key)
		}
		items[key] = definition
	}
	for _, definition := range items {
		seen := make(map[PermissionKey]struct{}, len(definition.Dependencies))
		for _, dependency := range definition.Dependencies {
			if !hasPermission(items, dependency) || dependency == definition.Key {
				return PermissionCatalog{}, fmt.Errorf("invalid dependency %q for permission %q", dependency, definition.Key)
			}
			if _, exists := seen[dependency]; exists {
				return PermissionCatalog{}, fmt.Errorf("duplicate dependency %q for permission %q", dependency, definition.Key)
			}
			seen[dependency] = struct{}{}
		}
	}
	for key := range items {
		if _, err := permissionDependencies(items, key, nil); err != nil {
			return PermissionCatalog{}, err
		}
	}
	return PermissionCatalog{items: items}, nil
}

func (c PermissionCatalog) WithCombinations(combinations ...PermissionCombination) PermissionCatalog {
	c.combinations = append([]PermissionCombination(nil), combinations...)
	for _, combination := range c.combinations {
		if combination.Key == "" || combination.LabelKey == "" || combination.Description == "" || len(combination.Permissions) == 0 {
			c.combinationError = fmt.Errorf("invalid permission combination %q", combination.Key)
			break
		}
		seen := make(map[PermissionKey]struct{}, len(combination.Permissions))
		for _, permission := range combination.Permissions {
			if !c.Has(permission) {
				c.combinationError = fmt.Errorf("unknown permission %q in combination %q", permission, combination.Key)
				break
			}
			if _, exists := seen[permission]; exists {
				c.combinationError = fmt.Errorf("duplicate permission %q in combination %q", permission, combination.Key)
				break
			}
			seen[permission] = struct{}{}
		}
		if c.combinationError != nil {
			break
		}
		if len(combination.Trigger) == 0 {
			c.combinationError = fmt.Errorf("empty trigger in combination %q", combination.Key)
			break
		}
		for _, trigger := range combination.Trigger {
			if _, exists := seen[trigger]; !exists {
				c.combinationError = fmt.Errorf("trigger permission %q is not in combination %q", trigger, combination.Key)
				break
			}
		}
		if c.combinationError != nil {
			break
		}
	}
	return c
}

func (c PermissionCatalog) Combinations() []PermissionCombination {
	result := append([]PermissionCombination(nil), c.combinations...)
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	for i := range result {
		result[i].Permissions = append([]PermissionKey(nil), result[i].Permissions...)
		result[i].Trigger = append([]PermissionKey(nil), result[i].Trigger...)
	}
	return result
}

func DefaultPermissionCatalog() PermissionCatalog {
	catalog, _ := NewPermissionCatalog(
		PermissionDefinition{Key: PermissionOnlineUsersRead, Resource: "online-users", Action: "read", LabelKey: "permissions.onlineUsers.read", Description: "View users with a valid sign-in session."},
		PermissionDefinition{Key: PermissionOnlineUsersWrite, Resource: "online-users", Action: "write", LabelKey: "permissions.onlineUsers.write", Description: "Sign users out of all devices."},
		PermissionDefinition{Key: PermissionUsersRead, Resource: "users", Action: "read", LabelKey: "permissions.users.read", Description: "View users and their assigned roles."},
		PermissionDefinition{Key: PermissionUsersWrite, Resource: "users", Action: "write", LabelKey: "permissions.users.write", Description: "Assign roles and deactivate, reactivate, or delete other users, including Super Admins."},
		PermissionDefinition{Key: PermissionRolesRead, Resource: "roles", Action: "read", LabelKey: "permissions.roles.read", Description: "View roles and their grants."},
		PermissionDefinition{Key: PermissionRolesWrite, Resource: "roles", Action: "write", LabelKey: "permissions.roles.write", Description: "Create, edit, and delete custom roles."},
		PermissionDefinition{Key: PermissionInvitationsRead, Resource: "invitations", Action: "read", LabelKey: "permissions.invitations.read", Description: "View invitations and their status."},
		PermissionDefinition{Key: PermissionInvitationsWrite, Resource: "invitations", Action: "write", LabelKey: "permissions.invitations.write", Description: "Create, resend, and revoke invitations."},
		PermissionDefinition{Key: PermissionSettingsRead, Resource: "settings", Action: "read", LabelKey: "permissions.settings.read", Description: "View system settings."},
		PermissionDefinition{Key: PermissionSettingsWrite, Resource: "settings", Action: "write", LabelKey: "permissions.settings.write", Description: "Update system settings."},
		PermissionDefinition{Key: PermissionOperationLogsRead, Resource: "operation-logs", Action: "read", LabelKey: "permissions.operationLogs.read", Description: "View operation history and recording status."},
		PermissionDefinition{Key: PermissionMailTasksRead, Resource: "mail-tasks", Action: "read", LabelKey: "permissions.mailTasks.read", Description: "View system mail tasks and delivery attempts."},
		PermissionDefinition{Key: PermissionMailTasksWrite, Resource: "mail-tasks", Action: "write", LabelKey: "permissions.mailTasks.write", Description: "Retry or permanently delete system mail tasks."},
	)
	return catalog.WithCombinations(
		PermissionCombination{Key: "mailTasks.manage", LabelKey: "permissions.combinations.mailTasksManage", Description: "View and manage system mail tasks.", Permissions: []PermissionKey{PermissionMailTasksRead, PermissionMailTasksWrite}, Trigger: []PermissionKey{PermissionMailTasksWrite}},
		PermissionCombination{Key: "invitations.create", LabelKey: "permissions.combinations.invitationsCreate", Description: "Create invitations and choose an assignable role.", Permissions: []PermissionKey{PermissionInvitationsWrite, PermissionRolesRead}, Trigger: []PermissionKey{PermissionInvitationsWrite}},
		PermissionCombination{Key: "invitations.manage", LabelKey: "permissions.combinations.invitationsManage", Description: "View and manage invitations with assignable roles.", Permissions: []PermissionKey{PermissionInvitationsRead, PermissionInvitationsWrite, PermissionRolesRead}, Trigger: []PermissionKey{PermissionInvitationsRead, PermissionInvitationsWrite}},
		PermissionCombination{Key: "users.assignRoles", LabelKey: "permissions.combinations.usersAssignRoles", Description: "View users and assign roles.", Permissions: []PermissionKey{PermissionUsersRead, PermissionUsersWrite, PermissionRolesRead}, Trigger: []PermissionKey{PermissionUsersWrite}},
		PermissionCombination{Key: "roles.manage", LabelKey: "permissions.combinations.rolesManage", Description: "View and manage roles.", Permissions: []PermissionKey{PermissionRolesRead, PermissionRolesWrite}, Trigger: []PermissionKey{PermissionRolesWrite}},
		PermissionCombination{Key: "settings.manage", LabelKey: "permissions.combinations.settingsManage", Description: "View and update system settings.", Permissions: []PermissionKey{PermissionSettingsRead, PermissionSettingsWrite}, Trigger: []PermissionKey{PermissionSettingsWrite}},
	)
}

const (
	PermissionOnlineUsersRead   PermissionKey = "online-users.read"
	PermissionOnlineUsersWrite  PermissionKey = "online-users.write"
	PermissionUsersRead         PermissionKey = "users.read"
	PermissionUsersWrite        PermissionKey = "users.write"
	PermissionRolesRead         PermissionKey = "roles.read"
	PermissionRolesWrite        PermissionKey = "roles.write"
	PermissionInvitationsRead   PermissionKey = "invitations.read"
	PermissionInvitationsWrite  PermissionKey = "invitations.write"
	PermissionSettingsRead      PermissionKey = "settings.read"
	PermissionSettingsWrite     PermissionKey = "settings.write"
	PermissionOperationLogsRead PermissionKey = "operation-logs.read"
	PermissionMailTasksRead     PermissionKey = "mail-tasks.read"
	PermissionMailTasksWrite    PermissionKey = "mail-tasks.write"
)

func (c PermissionCatalog) Has(key PermissionKey) bool { _, ok := c.items[key]; return ok }

func (c PermissionCatalog) Definitions() []PermissionDefinition {
	items := make([]PermissionDefinition, 0, len(c.items))
	for _, definition := range c.items {
		items = append(items, definition)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}

func (c PermissionCatalog) Keys() []PermissionKey {
	keys := make([]PermissionKey, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// Dependencies returns the transitive dependency set for a known permission.
// The returned keys are sorted and do not include key itself.
func (c PermissionCatalog) Dependencies(key PermissionKey) []PermissionKey {
	if !c.Has(key) {
		return nil
	}
	dependencies, err := permissionDependencies(c.items, key, nil)
	if err != nil {
		return nil
	}
	delete(dependencies, key)
	result := make([]PermissionKey, 0, len(dependencies))
	for dependency := range dependencies {
		result = append(result, dependency)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (c PermissionCatalog) Validate(keys []PermissionKey) ([]PermissionKey, error) {
	seen := make(map[PermissionKey]struct{}, len(keys))
	result := make([]PermissionKey, 0, len(keys))
	for _, key := range keys {
		if !c.Has(key) {
			return nil, &ValidationErrors{Items: []FieldError{{Field: "permissions", Code: "unknown_permission", Params: map[string]any{"key": string(key)}}}}
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		// Read and write grants are deliberately independent. Feature-level
		// combinations are validated explicitly by the application layer and
		// never expanded during authorization.
	}
	if len(seen) == 0 {
		return nil, &ValidationErrors{Items: []FieldError{{Field: "permissions", Code: "empty_permissions"}}}
	}
	result = result[:0]
	for key := range seen {
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

// ValidateFeatureSet checks that every write grant which represents a
// configured feature is accompanied by the complete permission combination
// needed to use that feature. The individual read/write keys remain
// independent at runtime; this validation only protects role definitions
// from persisting an unusable combination.
func (c PermissionCatalog) ValidateFeatureSet(keys []PermissionKey) ([]PermissionKey, error) {
	if c.combinationError != nil {
		return nil, c.combinationError
	}
	validated, err := c.Validate(keys)
	if err != nil {
		return nil, err
	}
	granted := make(map[PermissionKey]struct{}, len(validated))
	for _, key := range validated {
		granted[key] = struct{}{}
	}
	for _, combination := range c.combinations {
		triggered := true
		for _, trigger := range combination.Trigger {
			if _, exists := granted[trigger]; !exists {
				triggered = false
				break
			}
		}
		if !triggered {
			continue
		}
		missing := make([]string, 0)
		for _, key := range combination.Permissions {
			if _, exists := granted[key]; !exists {
				missing = append(missing, string(key))
			}
		}
		if len(missing) > 0 {
			return nil, &ValidationErrors{Items: []FieldError{{Field: "permissions", Code: "incomplete_permission_combination", Params: map[string]any{"combination": combination.Key, "missing": missing}}}}
		}
	}
	return validated, nil
}

func hasPermission(items map[PermissionKey]PermissionDefinition, key PermissionKey) bool {
	_, ok := items[key]
	return ok
}

func permissionDependencies(items map[PermissionKey]PermissionDefinition, key PermissionKey, visiting map[PermissionKey]bool) (map[PermissionKey]struct{}, error) {
	definition, ok := items[key]
	if !ok {
		return nil, fmt.Errorf("unknown permission dependency %q", key)
	}
	if visiting == nil {
		visiting = make(map[PermissionKey]bool)
	}
	if visiting[key] {
		return nil, fmt.Errorf("cyclic permission dependency %q", key)
	}
	visiting[key] = true
	result := map[PermissionKey]struct{}{key: {}}
	for _, dependency := range definition.Dependencies {
		dependencies, err := permissionDependencies(items, dependency, visiting)
		if err != nil {
			return nil, err
		}
		for item := range dependencies {
			result[item] = struct{}{}
		}
	}
	delete(visiting, key)
	return result, nil
}

type Role struct {
	ID              string
	SystemKey       string
	Name            string
	Description     string
	Permissions     []PermissionKey
	Revision        int64
	AssignmentCount int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (r Role) IsSystem() bool { return r.SystemKey != "" }

type Principal struct {
	User        User
	Roles       []Role
	Permissions []PermissionKey
	SuperAdmin  bool
}

func (p Principal) Has(key PermissionKey) bool {
	if p.SuperAdmin {
		return true
	}
	for _, item := range p.Permissions {
		if item == key {
			return true
		}
	}
	return false
}

func (p Principal) EffectivePermissions(catalog PermissionCatalog) []PermissionKey {
	if p.SuperAdmin {
		return catalog.Keys()
	}
	seen := make(map[PermissionKey]struct{})
	for _, key := range p.Permissions {
		if catalog.Has(key) {
			seen[key] = struct{}{}
		}
	}
	keys := make([]PermissionKey, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

type AccessUser struct {
	User        User
	Roles       []Role
	AuthVersion int64
}

type Invitation struct {
	ID             string
	Name           string
	Email          string
	Locale         Locale
	Roles          []Role
	ExpiresAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Revision       int64
	CreatedBy      string
	CreatedByName  string
	CreatedByEmail string
	CreatorDeleted bool
}

func CanonicalRoleName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
