package accountcenter

import (
	"testing"
	"time"
)

func TestPolicyMatrix(t *testing.T) {
	roles := []string{RoleFounder, RoleDeveloper, RoleManager, RoleMember}
	cases := []struct {
		name    string
		action  Action
		target  PolicyTarget
		allowed [4]bool // founder, developer, manager, member
	}{
		{"create team without separate grant", ActionCreateTeam, PolicyTarget{}, [4]bool{true, false, false, false}},
		{"create team with separate grant", ActionCreateTeam, PolicyTarget{TeamCreationGranted: true}, [4]bool{true, true, false, false}},
		{"manage assigned team", ActionManageTeam, PolicyTarget{TeamID: "team", InManagedTeam: true}, [4]bool{true, true, false, false}},
		{"manage other team", ActionManageTeam, PolicyTarget{TeamID: "team"}, [4]bool{true, false, false, false}},
		{"create founder", ActionCreateAccount, PolicyTarget{NewRole: RoleFounder}, [4]bool{}},
		{"create developer", ActionCreateAccount, PolicyTarget{NewRole: RoleDeveloper}, [4]bool{}},
		{"create manager", ActionCreateAccount, PolicyTarget{NewRole: RoleManager, InManagedTeam: true}, [4]bool{true, true, false, false}},
		{"create own member", ActionCreateAccount, PolicyTarget{NewRole: RoleMember, InManagedTeam: true}, [4]bool{true, true, true, false}},
		{"create other member", ActionCreateAccount, PolicyTarget{NewRole: RoleMember}, [4]bool{true, false, false, false}},
		{"grant developer to member", ActionGrantDeveloper, policyTestTarget(RoleMember, true), [4]bool{true, false, false, false}},
		{"grant developer to manager", ActionGrantDeveloper, policyTestTarget(RoleManager, true), [4]bool{true, false, false, false}},
		{"change developer grant", ActionGrantDeveloper, policyTestTarget(RoleDeveloper, true), [4]bool{true, false, false, false}},
		{"grant developer to founder", ActionGrantDeveloper, policyTestTarget(RoleFounder, true), [4]bool{}},
		{"promote member to manager", ActionSetRole, policyRoleChange(RoleMember, RoleManager), [4]bool{true, true, false, false}},
		{"promote member outside managed team", ActionSetRole, PolicyTarget{Identity: Identity{Username: "target", Role: RoleMember, Active: true}, NewRole: RoleManager}, [4]bool{true, false, false, false}},
		{"demote manager to member", ActionSetRole, policyRoleChange(RoleManager, RoleMember), [4]bool{true, true, false, false}},
		{"revoke developer role", ActionSetRole, policyRoleChange(RoleDeveloper, RoleMember), [4]bool{true, false, false, false}},
		{"promote to developer", ActionSetRole, policyRoleChange(RoleMember, RoleDeveloper), [4]bool{true, false, false, false}},
		{"change founder role", ActionSetRole, policyRoleChange(RoleFounder, RoleMember), [4]bool{}},
		{"assign founder role", ActionSetRole, policyRoleChange(RoleMember, RoleFounder), [4]bool{}},
		{"purge member", ActionPurgeAccount, policyTestTarget(RoleMember, true), [4]bool{true, false, false, false}},
		{"purge founder", ActionPurgeAccount, policyTestTarget(RoleFounder, true), [4]bool{}},
	}
	for _, tc := range cases {
		for i, role := range roles {
			t.Run(tc.name+"/"+role, func(t *testing.T) {
				actor := Identity{Username: "actor", Role: role, Active: true}
				if got := (Policy{}).Can(actor, tc.action, tc.target); got != tc.allowed[i] {
					t.Errorf("Can(%s, %s, %+v) = %t, want %t", role, tc.action, tc.target, got, tc.allowed[i])
				}
			})
		}
	}
}

func TestPolicyAccountOperationsByRoleAndTeam(t *testing.T) {
	cases := []struct {
		actor       string
		manage      [5]bool // founder, developer, manager, own member, other member
		resetSwitch [5]bool
	}{
		{RoleFounder, [5]bool{false, true, true, true, true}, [5]bool{false, true, true, true, true}},
		{RoleDeveloper, [5]bool{false, false, true, true, false}, [5]bool{false, false, true, true, true}},
		{RoleManager, [5]bool{false, false, false, true, false}, [5]bool{false, false, false, true, false}},
		{RoleMember, [5]bool{}, [5]bool{}},
	}
	targets := []PolicyTarget{
		policyTestTarget(RoleFounder, true), policyTestTarget(RoleDeveloper, true),
		policyTestTarget(RoleManager, true), policyTestTarget(RoleMember, true), policyTestTarget(RoleMember, false),
	}
	for _, tc := range cases {
		for i, target := range targets {
			actor := Identity{Username: "actor", Role: tc.actor, Active: true}
			for _, check := range []struct {
				action Action
				want   bool
			}{{ActionManageAccount, tc.manage[i]}, {ActionResetPassword, tc.resetSwitch[i]}, {ActionImpersonate, tc.resetSwitch[i]}} {
				if got := (Policy{}).Can(actor, check.action, target); got != check.want {
					t.Errorf("Can(%s, %s, %s, in managed team=%t) = %t, want %t", tc.actor, check.action, target.Identity.Role, target.InManagedTeam, got, check.want)
				}
			}
		}
	}
}

func TestPolicyRejectsInvalidActorsAndTargets(t *testing.T) {
	now := time.Now()
	founder := Identity{Username: "actor", Role: RoleFounder, Active: true}
	member := policyTestTarget(RoleMember, true)
	for _, actor := range []Identity{
		{Role: RoleFounder, Active: true},
		{Username: "actor", Role: RoleFounder},
		{Username: "actor", Role: RoleFounder, Active: true, ArchivedAt: &now},
		{Username: "actor", Role: "dev", Active: true},
	} {
		if (Policy{}).Can(actor, ActionResetPassword, member) {
			t.Errorf("invalid actor was authorized: %+v", actor)
		}
	}
	for _, target := range []PolicyTarget{{}, policyTestTarget("invalid", true), {Identity: founder}, {Identity: Identity{Username: "ACTOR", Role: RoleMember, Active: true}}} {
		if (Policy{}).Can(founder, ActionResetPassword, target) {
			t.Errorf("invalid target was authorized: %+v", target)
		}
	}
	if (Policy{}).Can(founder, "unknown_action", member) {
		t.Error("unknown action was authorized")
	}
	if (Policy{}).Can(founder, ActionManageTeam, PolicyTarget{InManagedTeam: true}) {
		t.Error("missing team was authorized")
	}
	member.Identity.Active = false
	if (Policy{}).Can(founder, ActionImpersonate, member) {
		t.Error("inactive impersonation target was authorized")
	}
	member.Identity.Active, member.Identity.ArchivedAt = true, &now
	if (Policy{}).Can(founder, ActionImpersonate, member) {
		t.Error("archived impersonation target was authorized")
	}
	if !(Policy{}).Can(founder, ActionManageAccount, member) {
		t.Error("archived member cannot be managed for restore")
	}
}

func policyTestTarget(role string, inManagedTeam bool) PolicyTarget {
	return PolicyTarget{Identity: Identity{Username: "target", Role: role, Active: true}, InManagedTeam: inManagedTeam}
}

func policyRoleChange(fromRole, toRole string) PolicyTarget {
	target := policyTestTarget(fromRole, true)
	target.NewRole = toRole
	return target
}
